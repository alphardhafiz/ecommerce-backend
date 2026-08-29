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
	return NewCart(service.NewCartService(repository.NewCartRepo(pool), repository.NewProductRepo(pool))), pool
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

func seedCartItem(t *testing.T, pool *pgxpool.Pool, cartID, productID string, qty int) string {
	t.Helper()
	repo := repository.NewCartRepo(pool)
	if err := repo.AddItem(context.Background(), cartID, productID, qty); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := pool.QueryRow(context.Background(),
		`SELECT id FROM cart_items WHERE cart_id = $1 AND product_id = $2`, cartID, productID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
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
	deleted := seedProduct(t, pool, "Dihapus", 50000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, deleted.ID)

	prodRepo := repository.NewProductRepo(pool)
	if _, err := prodRepo.SetActive(context.Background(), inactive.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := prodRepo.SoftDelete(context.Background(), deleted.ID); err != nil {
		t.Fatal(err)
	}

	seedCartItem(t, pool, cart.ID, good.ID, 3)
	seedCartItem(t, pool, cart.ID, inactive.ID, 2)
	seedCartItem(t, pool, cart.ID, deleted.ID, 1)

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
	if len(body.Data.Items) != 3 {
		t.Fatalf("items = %+v, want all 3 listed (flagged)", body.Data.Items)
	}
	if body.Data.Total != 30000 {
		t.Errorf("total = %d, want 30000 (only available item: 10000*3)", body.Data.Total)
	}
	for _, item := range body.Data.Items {
		if (item.ProductID == inactive.ID || item.ProductID == deleted.ID) && item.IsAvailable {
			t.Errorf("product %s must be is_available=false", item.ProductID)
		}
	}
}

func TestCartGetIncludesPrimaryImage(t *testing.T) {
	h, pool := newCartHandler(t)
	email := "cart-img@example.com"
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

	withImg := seedProduct(t, pool, "Dengan Gambar", 10000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, withImg.ID)
	noImg := seedProduct(t, pool, "Tanpa Gambar", 20000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, noImg.ID)

	seedProductImage(t, pool, withImg.ID, "https://example.com/img.png", true, 0)
	seedCartItem(t, pool, cart.ID, withImg.ID, 1)
	seedCartItem(t, pool, cart.ID, noImg.ID, 1)

	rec := cartRequest(t, h, userToken(t, userID, "user"))
	var body struct {
		Data struct {
			Items []struct {
				ProductID    string  `json:"product_id"`
				PrimaryImage *string `json:"primary_image"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(body.Data.Items))
	}
	for _, item := range body.Data.Items {
		switch item.ProductID {
		case withImg.ID:
			if item.PrimaryImage == nil || *item.PrimaryImage != "https://example.com/img.png" {
				t.Errorf("primary_image = %v, want image URL", item.PrimaryImage)
			}
		case noImg.ID:
			if item.PrimaryImage != nil {
				t.Errorf("primary_image = %v, want null", item.PrimaryImage)
			}
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

func cartItemRequest(t *testing.T, h *Cart, method, itemID, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/cart/items"
	if itemID != "" {
		path += "/" + itemID
	}
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if itemID != "" {
		req.SetPathValue("id", itemID)
	}
	rec := httptest.NewRecorder()
	auth := middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))
	var hf func(http.ResponseWriter, *http.Request)
	switch method {
	case http.MethodPost:
		hf = h.AddItem
	case http.MethodPatch:
		hf = h.UpdateItemQty
	default:
		hf = h.RemoveItem
	}
	auth(http.HandlerFunc(hf)).ServeHTTP(rec, req)
	return rec
}

func cartClearRequest(t *testing.T, h *Cart, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/cart", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))(http.HandlerFunc(h.Clear)).ServeHTTP(rec, req)
	return rec
}

func TestCartAddItemMergesQuantity(t *testing.T) {
	h, pool := newCartHandler(t)
	email := "cart-add-item@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
	defer pool.Exec(context.Background(), `DELETE FROM carts WHERE user_id = $1`, userID)

	prod := seedProduct(t, pool, "Produk Cart", 10000, 10, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, prod.ID)

	token := userToken(t, userID, "user")

	rec := cartItemRequest(t, h, http.MethodPost, "", token, `{"product_id":"`+prod.ID+`","quantity":2}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}

	rec = cartItemRequest(t, h, http.MethodPost, "", token, `{"product_id":"`+prod.ID+`","quantity":3}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("merge add status = %d, want 201", rec.Code)
	}

	var qty int
	pool.QueryRow(context.Background(),
		`SELECT ci.quantity FROM cart_items ci JOIN carts c ON c.id = ci.cart_id WHERE c.user_id = $1 AND ci.product_id = $2`,
		userID, prod.ID).Scan(&qty)
	if qty != 5 {
		t.Errorf("quantity = %d, want 5 (merged)", qty)
	}
}

func TestCartAddItemValidation(t *testing.T) {
	h, pool := newCartHandler(t)
	email := "cart-add-invalid@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)

	token := userToken(t, userID, "user")

	// quantity 0
	rec := cartItemRequest(t, h, http.MethodPost, "", token, `{"product_id":"00000000-0000-0000-0000-000000000000","quantity":0}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("qty=0 status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "VALIDATION_ERROR") {
		t.Errorf("body = %s, want VALIDATION_ERROR", rec.Body.String())
	}

	// missing product
	rec = cartItemRequest(t, h, http.MethodPost, "", token, `{"product_id":"00000000-0000-0000-0000-000000000000","quantity":1}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing product status = %d, want 404", rec.Code)
	}

	// soft-deleted product
	prod := seedProduct(t, pool, "Dihapus", 10000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, prod.ID)
	prodRepo := repository.NewProductRepo(pool)
	if err := prodRepo.SoftDelete(context.Background(), prod.ID); err != nil {
		t.Fatal(err)
	}
	rec = cartItemRequest(t, h, http.MethodPost, "", token, `{"product_id":"`+prod.ID+`","quantity":1}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("soft-deleted product status = %d, want 404", rec.Code)
	}
}

func TestCartUpdateQuantity(t *testing.T) {
	h, pool := newCartHandler(t)
	email := "cart-update-qty@example.com"
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

	prod := seedProduct(t, pool, "Produk Qty", 10000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, prod.ID)

	itemID := seedCartItem(t, pool, cart.ID, prod.ID, 2)

	token := userToken(t, userID, "user")
	rec := cartItemRequest(t, h, http.MethodPatch, itemID, token, `{"quantity":4}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var qty int
	pool.QueryRow(context.Background(), `SELECT quantity FROM cart_items WHERE id = $1`, itemID).Scan(&qty)
	if qty != 4 {
		t.Errorf("quantity = %d, want 4", qty)
	}

	// exceeds stock -> 400
	rec = cartItemRequest(t, h, http.MethodPatch, itemID, token, `{"quantity":10}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("over-stock status = %d, want 400", rec.Code)
	}

	// quantity 0 -> 400
	rec = cartItemRequest(t, h, http.MethodPatch, itemID, token, `{"quantity":0}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("qty=0 status = %d, want 400", rec.Code)
	}
}

func TestCartUpdateQuantityForbidden(t *testing.T) {
	h, pool := newCartHandler(t)
	ownerEmail := "cart-owner@example.com"
	otherEmail := "cart-other@example.com"
	seedUser(t, pool, ownerEmail, "abc12345", "active")
	seedUser(t, pool, otherEmail, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = ANY($1)`, []string{ownerEmail, otherEmail})

	var ownerID, otherID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, ownerEmail).Scan(&ownerID)
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, otherEmail).Scan(&otherID)

	catRepo := repository.NewCartRepo(pool)
	cart, err := catRepo.GetOrCreate(context.Background(), ownerID)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM carts WHERE user_id = $1`, ownerID)

	prod := seedProduct(t, pool, "Produk Forbidden", 10000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, prod.ID)

	itemID := seedCartItem(t, pool, cart.ID, prod.ID, 1)

	// other user tries to update owner's item
	rec := cartItemRequest(t, h, http.MethodPatch, itemID, userToken(t, otherID, "user"), `{"quantity":3}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestCartRemoveItemAndClear(t *testing.T) {
	h, pool := newCartHandler(t)
	email := "cart-rm-clear@example.com"
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

	p1 := seedProduct(t, pool, "Produk A", 10000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, p1.ID)
	p2 := seedProduct(t, pool, "Produk B", 10000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, p2.ID)

	item1 := seedCartItem(t, pool, cart.ID, p1.ID, 1)
	item2 := seedCartItem(t, pool, cart.ID, p2.ID, 1)

	token := userToken(t, userID, "user")

	// remove one item
	rec := cartItemRequest(t, h, http.MethodDelete, item1, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("remove status = %d, want 200", rec.Code)
	}
	var n int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM cart_items WHERE id = $1`, item1).Scan(&n)
	if n != 0 {
		t.Error("removed item must be deleted")
	}

	// clear rest
	rec = cartClearRequest(t, h, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear status = %d, want 200", rec.Code)
	}
	pool.QueryRow(context.Background(), `SELECT count(*) FROM cart_items WHERE id = $1`, item2).Scan(&n)
	if n != 0 {
		t.Error("clear must remove remaining items")
	}
}

func TestCartMutationsUnauthorized(t *testing.T) {
	h, _ := newCartHandler(t)
	if rec := cartItemRequest(t, h, http.MethodPost, "", "", `{"product_id":"x","quantity":1}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("add status = %d, want 401", rec.Code)
	}
	if rec := cartClearRequest(t, h, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("clear status = %d, want 401", rec.Code)
	}
}
