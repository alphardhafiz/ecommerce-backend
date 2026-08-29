package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	jwtpkg "ecommerce/server/internal/jwt"
	"ecommerce/server/internal/middleware"
	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/service"
)

func newDashboardHandler(t *testing.T) (*Dashboard, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping dashboard test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return NewDashboard(service.NewDashboardService(repository.NewDashboardRepo(pool))), pool
}

// seedDashboardFixture: 2 active + 1 inactive user; 3 active + 1 inactive
// product with two low-stock (stock 2 and 5); orders: 1 PENDING (100000,
// today), 1 PAID (50000, today), 1 COMPLETED (75000, 40 days ago). Returns
// the low-stock product ids (incl. the inactive one).
func seedDashboardFixture(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	ctx := context.Background()

	seedUser(t, pool, "dash-a@example.com", "abc12345", "active")
	seedUser(t, pool, "dash-b@example.com", "abc12345", "active")
	seedUser(t, pool, "dash-c@example.com", "abc12345", "inactive")

	seedProduct(t, pool, "Dash Normal", 10000, 50, nil)
	low1 := seedProduct(t, pool, "Dash Low", 20000, 2, nil)
	low2 := seedProduct(t, pool, "Dash Low2", 20000, 5, nil)
	lowInactive := seedProduct(t, pool, "Dash Low Inactive", 20000, 1, nil)
	pool.Exec(ctx, `UPDATE products SET is_active = false WHERE id = $1`, lowInactive.ID)

	var userID string
	pool.QueryRow(ctx, `SELECT id FROM users WHERE email = 'dash-a@example.com'`).Scan(&userID)

	for _, o := range []struct {
		status  string
		amount  int64
		daysAgo int
	}{
		{"PENDING", 100000, 0},
		{"PAID", 50000, 0},
		{"COMPLETED", 75000, 40},
	} {
		pool.Exec(ctx,
			`INSERT INTO orders (user_id, status, total_amount, recipient_name, phone, shipping_address, created_at)
			 VALUES ($1, $2, $3, 'Budi', '08123456789', 'Jl. Merdeka', now() - make_interval(days => $4))`,
			userID, o.status, o.amount, o.daysAgo)
	}
	return []string{low1.ID, low2.ID, lowInactive.ID}
}

func cleanupDashboardFixture(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool.Exec(ctx, `DELETE FROM orders WHERE user_id IN (SELECT id FROM users WHERE email LIKE 'dash-%@example.com')`)
	pool.Exec(ctx, `DELETE FROM users WHERE email LIKE 'dash-%@example.com'`)
	pool.Exec(ctx, `DELETE FROM products WHERE name LIKE 'Dash %'`)
}

func dashboardRequest(t *testing.T, h *Dashboard, token, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard"+query, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	return rec
}

func dashboardData(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Data
}

// n returns the float64 metric from a dashboard payload (0 when absent).
func n(data map[string]any, key string) float64 {
	if v, ok := data[key].(float64); ok {
		return v
	}
	return 0
}

func TestDashboardMetrics(t *testing.T) {
	h, pool := newDashboardHandler(t)

	seedUser(t, pool, "dash-admin@example.com", "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = 'dash-admin@example.com'`)
	var adminID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = 'dash-admin@example.com'`).Scan(&adminID)
	token := userToken(t, adminID, "admin")

	// baseline (DB may hold data from other tests), then assert deltas
	base := dashboardData(t, dashboardRequest(t, h, token, ""))

	lowIDs := seedDashboardFixture(t, pool)
	defer cleanupDashboardFixture(t, pool)

	rec := dashboardRequest(t, h, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	data := dashboardData(t, rec)

	// 2 active users added; inactive does not count
	if got := n(data, "total_users") - n(base, "total_users"); got != 2 {
		t.Errorf("total_users delta = %v, want 2", got)
	}
	// 3 active products added; inactive low-stock does not count
	if got := n(data, "total_products") - n(base, "total_products"); got != 3 {
		t.Errorf("total_products delta = %v, want 3", got)
	}
	// 3 orders added
	if got := n(data, "total_orders") - n(base, "total_orders"); got != 3 {
		t.Errorf("total_orders delta = %v, want 3", got)
	}
	byStatus, _ := data["orders_by_status"].(map[string]any)
	baseStatus, _ := base["orders_by_status"].(map[string]any)
	for _, st := range []string{"PENDING", "PAID", "COMPLETED"} {
		want := float64(1)
		got := n(byStatus, st) - n(baseStatus, st)
		if got != want {
			t.Errorf("%s delta = %v, want 1", st, got)
		}
	}
	// revenue: PAID + COMPLETED (PENDING excluded)
	if got := n(data, "revenue") - n(base, "revenue"); got != 125000 {
		t.Errorf("revenue delta = %v, want 125000", got)
	}
	// low-stock list properties: every item has stock <= 5, and the
	// inactive product is excluded (DB may hold other low-stock products,
	// so the list is checked by property, not by exact membership)
	lowStock, _ := data["low_stock"].([]any)
	if len(lowStock) == 0 {
		t.Fatal("low_stock list is empty")
	}
	for _, item := range lowStock {
		entry := item.(map[string]any)
		if entry["stock"].(float64) > 5 {
			t.Errorf("low_stock item %v has stock > 5", entry["name"])
		}
		if entry["id"] == lowIDs[2] {
			t.Error("inactive product must not appear in low_stock")
		}
	}
	// both active fixture products are low stock in the DB (delta)
	var activeLow int
	pool.QueryRow(context.Background(),
		`SELECT count(*) FROM products WHERE name LIKE 'Dash Low%' AND is_active AND deleted_at IS NULL AND stock <= 5`).Scan(&activeLow)
	if activeLow != 2 {
		t.Errorf("active low-stock fixture products = %d, want 2", activeLow)
	}

	// period 7d: only today's orders (PENDING + PAID), COMPLETED excluded
	rec = dashboardRequest(t, h, token, "?period=7d")
	data = dashboardData(t, rec)
	if got := n(data, "total_orders") - n(base, "total_orders"); got != 2 {
		t.Errorf("7d total_orders delta = %v, want 2", got)
	}
	if got := n(data, "revenue") - n(base, "revenue"); got != 50000 {
		t.Errorf("7d revenue delta = %v, want 50000", got)
	}

	// custom range covering only the old COMPLETED order
	oldDay := time.Now().AddDate(0, 0, -41).Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	rec = dashboardRequest(t, h, token, "?start_date="+oldDay+"&end_date="+yesterday)
	data = dashboardData(t, rec)
	if got := n(data, "total_orders") - n(base, "total_orders"); got != 1 {
		t.Errorf("custom total_orders delta = %v, want 1", got)
	}
	if got := n(data, "revenue") - n(base, "revenue"); got != 75000 {
		t.Errorf("custom revenue delta = %v, want 75000", got)
	}

	// today's range: both today orders, old one excluded
	today := time.Now().Format("2006-01-02")
	rec = dashboardRequest(t, h, token, "?start_date="+today+"&end_date="+today)
	data = dashboardData(t, rec)
	if got := n(data, "total_orders") - n(base, "total_orders"); got != 2 {
		t.Errorf("today total_orders delta = %v, want 2", got)
	}

	// invalid period -> 400
	rec = dashboardRequest(t, h, token, "?period=bogus")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid period status = %d, want 400", rec.Code)
	}
	// invalid date -> 400
	rec = dashboardRequest(t, h, token, "?start_date=01-01-2026")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid start_date status = %d, want 400", rec.Code)
	}
}

func TestDashboardForbiddenForUser(t *testing.T) {
	h, pool := newDashboardHandler(t)
	seedUser(t, pool, "dash-noadmin@example.com", "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = 'dash-noadmin@example.com'`)
	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = 'dash-noadmin@example.com'`).Scan(&userID)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+userToken(t, userID, "user"))
	rec := httptest.NewRecorder()
	middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))(middleware.RequireRole("admin")(http.HandlerFunc(h.Get))).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "FORBIDDEN") {
		t.Errorf("body = %s, want FORBIDDEN", rec.Body.String())
	}
}
