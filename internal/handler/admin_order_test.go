package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	jwtpkg "ecommerce/server/internal/jwt"
	"ecommerce/server/internal/middleware"
	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/service"
)

func adminOrderHandler(t *testing.T) (*Order, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping admin order test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return NewOrder(service.NewOrderService(repository.NewOrderRepo(pool))), pool
}

func adminOrdersRequest(t *testing.T, h *Order, method, id, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/admin/orders", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if id != "" {
		req.SetPathValue("id", id)
	}
	rec := httptest.NewRecorder()
	var handlerFn func(http.ResponseWriter, *http.Request)
	if method == http.MethodPatch {
		handlerFn = h.UpdateStatus
	} else {
		handlerFn = h.ListAll
	}
	admin := middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))(middleware.RequireRole("admin")(http.HandlerFunc(handlerFn)))
	admin.ServeHTTP(rec, req)
	return rec
}

// seedOrderForUser creates a PENDING order for the given user via checkout
// flow and returns its id.
func seedOrderForUser(t *testing.T, h *Order, pool *pgxpool.Pool, email string) string {
	t.Helper()
	userID, itemID, addrID, _ := seedCheckoutUser(t, pool, email, 40000, 7, 1)
	rec := checkoutRequest(t, h, userToken(t, userID, "user"),
		`{"cart_item_ids":["`+itemID+`"],"address_id":"`+addrID+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("checkout status = %d, body: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			OrderID string `json:"order_id"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Data.OrderID
}

func TestAdminOrdersListFilterAndPagination(t *testing.T) {
	h, pool := adminOrderHandler(t)
	adminEmail := "admin-orders@example.com"
	userEmail1 := "admin-orders-u1@example.com"
	userEmail2 := "admin-orders-u2@example.com"
	seedUser(t, pool, adminEmail, "abc12345", "active")
	defer cleanupOrderUser(t, pool, adminEmail)
	defer cleanupOrderUser(t, pool, userEmail1)
	defer cleanupOrderUser(t, pool, userEmail2)

	var adminID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, adminEmail).Scan(&adminID)
	adminToken := userToken(t, adminID, "admin")

	// baseline PENDING count: DB may hold manual E2E orders, assert deltas
	var basePending int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM orders WHERE status = 'PENDING'`).Scan(&basePending)

	seedOrderForUser(t, h, pool, userEmail1)
	seedOrderForUser(t, h, pool, userEmail2)

	rec := adminOrdersRequest(t, h, http.MethodGet, "", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Meta.Total < basePending+2 || len(body.Data) < 2 {
		t.Fatalf("want at least 2 orders from both users, got total=%d len=%d", body.Meta.Total, len(body.Data))
	}

	// filter by status: all seed orders are PENDING
	rec = adminOrdersRequest(t, h, http.MethodGet, "", adminToken, "")
	q := `?status=PENDING`
	req := httptest.NewRequest(http.MethodGet, "/admin/orders"+q, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec2 := httptest.NewRecorder()
	middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))(middleware.RequireRole("admin")(http.HandlerFunc(h.ListAll))).ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("filter status = %d", rec2.Code)
	}
	var filtered struct {
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &filtered); err != nil {
		t.Fatal(err)
	}
	if filtered.Meta.Total != basePending+2 {
		t.Errorf("filtered total = %d, want %d (baseline + 2)", filtered.Meta.Total, basePending+2)
	}

	// invalid status -> 400
	req = httptest.NewRequest(http.MethodGet, "/admin/orders?status=BOGUS", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rec3 := httptest.NewRecorder()
	middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))(middleware.RequireRole("admin")(http.HandlerFunc(h.ListAll))).ServeHTTP(rec3, req)
	if rec3.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d, want 400", rec3.Code)
	}
}

func TestAdminOrdersUpdateStatusTransitions(t *testing.T) {
	h, pool := adminOrderHandler(t)
	adminEmail := "admin-status@example.com"
	userEmail := "admin-status-u@example.com"
	seedUser(t, pool, adminEmail, "abc12345", "active")
	defer cleanupOrderUser(t, pool, adminEmail)
	defer cleanupOrderUser(t, pool, userEmail)

	var adminID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, adminEmail).Scan(&adminID)
	adminToken := userToken(t, adminID, "admin")

	orderID := seedOrderForUser(t, h, pool, userEmail)

	// PENDING -> PAID is not an admin transition -> 409
	rec := adminOrdersRequest(t, h, http.MethodPatch, orderID, adminToken, `{"status":"PAID"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("PENDING->PAID = %d, want 409", rec.Code)
	}

	// force to PAID (webhook would do this in Fase 6)
	pool.Exec(context.Background(), `UPDATE orders SET status = 'PAID' WHERE id = $1`, orderID)

	// PAID -> PROCESSING -> SHIPPED -> COMPLETED (valid chain)
	for _, st := range []string{"PROCESSING", "SHIPPED", "COMPLETED"} {
		rec = adminOrdersRequest(t, h, http.MethodPatch, orderID, adminToken, `{"status":"`+st+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("->%s = %d, want 200, body: %s", st, rec.Code, rec.Body.String())
		}
	}

	// COMPLETED is final -> any transition 409
	rec = adminOrdersRequest(t, h, http.MethodPatch, orderID, adminToken, `{"status":"CANCELLED"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("COMPLETED->CANCELLED = %d, want 409", rec.Code)
	}

	// invalid status value -> 400
	rec = adminOrdersRequest(t, h, http.MethodPatch, orderID, adminToken, `{"status":"BOGUS"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bogus status = %d, want 400", rec.Code)
	}

	// missing order -> 404
	rec = adminOrdersRequest(t, h, http.MethodPatch, "00000000-0000-0000-0000-000000000000", adminToken, `{"status":"PROCESSING"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing = %d, want 404", rec.Code)
	}
}

func TestAdminOrdersCancelRestoresStock(t *testing.T) {
	h, pool := adminOrderHandler(t)
	adminEmail := "admin-cancel@example.com"
	userEmail := "admin-cancel-u@example.com"
	seedUser(t, pool, adminEmail, "abc12345", "active")
	defer cleanupOrderUser(t, pool, adminEmail)
	defer cleanupOrderUser(t, pool, userEmail)

	var adminID, userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, adminEmail).Scan(&adminID)
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, userEmail).Scan(&userID)
	adminToken := userToken(t, adminID, "admin")

	orderID := seedOrderForUser(t, h, pool, userEmail)

	var prodID string
	pool.QueryRow(context.Background(),
		`SELECT product_id FROM order_items WHERE order_id = $1`, orderID).Scan(&prodID)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, prodID)

	var stockAfterCheckout int
	pool.QueryRow(context.Background(), `SELECT stock FROM products WHERE id = $1`, prodID).Scan(&stockAfterCheckout)

	pool.Exec(context.Background(), `UPDATE orders SET status = 'PAID' WHERE id = $1`, orderID)
	rec := adminOrdersRequest(t, h, http.MethodPatch, orderID, adminToken, `{"status":"CANCELLED"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PAID->CANCELLED = %d, want 200", rec.Code)
	}

	var stock int
	pool.QueryRow(context.Background(), `SELECT stock FROM products WHERE id = $1`, prodID).Scan(&stock)
	if stock != stockAfterCheckout+1 {
		t.Errorf("stock = %d, want %d (restored)", stock, stockAfterCheckout+1)
	}
	var status string
	pool.QueryRow(context.Background(), `SELECT status FROM orders WHERE id = $1`, orderID).Scan(&status)
	if status != "CANCELLED" {
		t.Errorf("status = %s, want CANCELLED", status)
	}
}

func TestAdminOrdersForbiddenForUser(t *testing.T) {
	h, pool := adminOrderHandler(t)
	userEmail := "admin-forbid@example.com"
	seedUser(t, pool, userEmail, "abc12345", "active")
	defer cleanupOrderUser(t, pool, userEmail)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, userEmail).Scan(&userID)

	req := httptest.NewRequest(http.MethodGet, "/admin/orders", nil)
	req.Header.Set("Authorization", "Bearer "+userToken(t, userID, "user"))
	rec := httptest.NewRecorder()
	middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))(middleware.RequireRole("admin")(http.HandlerFunc(h.ListAll))).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/orders", nil)
	rec = httptest.NewRecorder()
	middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))(middleware.RequireRole("admin")(http.HandlerFunc(h.ListAll))).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "UNAUTHORIZED") {
		t.Errorf("body = %s, want UNAUTHORIZED", rec.Body.String())
	}
}
