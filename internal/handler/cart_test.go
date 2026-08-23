package handler

import (
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

func newCartHandler(t *testing.T) (*Cart, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping cart handler test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return NewCart(service.NewCartService(repository.NewCartRepo(pool))), pool
}

func cartRequest(t *testing.T, h *Cart, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/cart", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))(http.HandlerFunc(h.Get)).ServeHTTP(rec, req)
	return rec
}

func seedCartItem(t *testing.T, pool *pgxpool.Pool, cartID, productID string, qty int) {
	t.Helper()
	repo := repository.NewCartRepo(pool)
	if err := repo.AddItem(context.Background(), cartID, productID, qty); err != nil {
		t.Fatal(err)
	}
}

func TestCartGetEmptyLazyCreates(t *testing.T) {
	h, pool := newCartHandler(t)
	email := "cart-get-empty@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	defer pool.Exec(context.Background(), `DELETE FROM carts WHERE user_id = $1`, userID)

	rec := cartRequest(t, h, userToken(t, userID, "user"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Items []any `json:"items"`
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || len(body.Data.Items) != 0 || body.Data.Total != 0 {
		t.Errorf("body = %+v, want empty items + total 0", body.Data)
	}

	// cart must now exist (lazy creation)
	var n int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM carts WHERE user_id = $1`, userID).Scan(&n)
	if n != 1 {
		t.Errorf("cart count = %d, want 1 (lazy created)", n)
	}
}

func TestCartGetWithItems(t *testing.T) {
	h, pool := newCartHandler(t)
	email := "cart-get-items@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)

	catRepo := repository.NewCartRepo(pool)
	cart, err := catRepo.GetOrCreate(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM carts WHERE user_id = $1`, userID)

	p1 := seedProduct(t, pool, "Kaos Polos", 89000, 10, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, p1.ID)
	p2 := seedProduct(t, pool, "Celana Jeans", 150000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, p2.ID)

	seedCartItem(t, pool, cart.ID, p1.ID, 2)
	seedCartItem(t, pool, cart.ID, p2.ID, 1)

	rec := cartRequest(t, h, userToken(t, userID, "user"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Data struct {
			Items []struct {
				ID          string `json:"id"`
				ProductID   string `json:"product_id"`
				Name        string `json:"name"`
				Price       int64  `json:"price"`
				Quantity    int    `json:"quantity"`
				Subtotal    int64  `json:"subtotal"`
				IsAvailable bool   `json:"is_available"`
			} `json:"items"`
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.Items) != 2 {
		t.Fatalf("items = %+v, want 2", body.Data.Items)
	}
	// subtotal on-the-fly: 89000*2 + 150000*1 = 328000
	if body.Data.Total != 328000 {
		t.Errorf("total = %d, want 328000", body.Data.Total)
	}
	found := map[string]bool{}
	for _, item := range body.Data.Items {
		found[item.ProductID] = true
		if !item.IsAvailable {
			t.Errorf("item %s must be available", item.ProductID)
		}
	}
	if !found[p1.ID] || !found[p2.ID] {
		t.Errorf("items must contain both products, got %+v", body.Data.Items)
	}
}

func TestCartGetExcludesUnavailableFromTotal(t *testing.T) {
	h, pool := newCartHandler(t)
	email := "cart-get-unavail@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)

	catRepo := repository.NewCartRepo(pool)
	cart, err := catRepo.GetOrCreate(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM carts WHERE user_id = $1`, userID)

	good := seedProduct(t, pool, "Aktif", 10000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, good.ID)
	inactive := seedProduct(t, pool, "Nonaktif", 50000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, inactive.ID)

	prodRepo := repository.NewProductRepo(pool)
	if _, err := prodRepo.SetActive(context.Background(), inactive.ID, false); err != nil {
		t.Fatal(err)
	}

	seedCartItem(t, pool, cart.ID, good.ID, 3)
	seedCartItem(t, pool, cart.ID, inactive.ID, 2)

	rec := cartRequest(t, h, userToken(t, userID, "user"))
	var body struct {
		Data struct {
			Items []struct {
				ProductID   string `json:"product_id"`
				IsAvailable bool   `json:"is_available"`
			} `json:"items"`
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.Items) != 2 {
		t.Fatalf("items = %+v, want both listed", body.Data.Items)
	}
	if body.Data.Total != 30000 {
		t.Errorf("total = %d, want 30000 (only available item: 10000*3)", body.Data.Total)
	}
	for _, item := range body.Data.Items {
		if item.ProductID == inactive.ID && item.IsAvailable {
			t.Error("inactive product must be is_available=false")
		}
	}
}

func TestCartGetUnauthorized(t *testing.T) {
	h, _ := newCartHandler(t)
	rec := cartRequest(t, h, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "UNAUTHORIZED") {
		t.Errorf("body = %s, want UNAUTHORIZED", rec.Body.String())
	}
}
