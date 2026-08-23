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

func newWishlistHandler(t *testing.T) (*Wishlist, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping wishlist handler test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return NewWishlist(service.NewWishlistService(repository.NewWishlistRepo(pool))), pool
}

func wishlistRequest(t *testing.T, h *Wishlist, method, productID, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/wishlist"
	if productID != "" {
		path += "/" + productID
	}
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if productID != "" {
		req.SetPathValue("productId", productID)
	}
	rec := httptest.NewRecorder()
	auth := middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))
	var hf func(http.ResponseWriter, *http.Request)
	switch method {
	case http.MethodPost:
		hf = h.Add
	case http.MethodDelete:
		hf = h.Remove
	default:
		hf = h.List
	}
	auth(http.HandlerFunc(hf)).ServeHTTP(rec, req)
	return rec
}

func TestWishlistAddAndList(t *testing.T) {
	h, pool := newWishlistHandler(t)
	email := "wishlist-user@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)

	prod := seedProduct(t, pool, "Produk Wishlist", 10000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, prod.ID)
	defer pool.Exec(context.Background(), `DELETE FROM wishlists WHERE user_id = $1`, userID)

	token := userToken(t, userID, "user")

	rec := wishlistRequest(t, h, http.MethodPost, "", token, `{"product_id":"`+prod.ID+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}

	rec = wishlistRequest(t, h, http.MethodGet, "", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rec.Code)
	}

	var body struct {
		Success bool `json:"success"`
		Data    []struct {
			ProductID   string `json:"product_id"`
			ProductName string `json:"product_name"`
			Price       int64  `json:"price"`
			InStock     bool   `json:"in_stock"`
			IsActive    bool   `json:"is_active"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || len(body.Data) != 1 {
		t.Fatalf("list = %+v, want 1 item", body)
	}
	if body.Data[0].ProductID != prod.ID || body.Data[0].ProductName != "Produk Wishlist" || body.Data[0].Price != 10000 || !body.Data[0].InStock || !body.Data[0].IsActive {
		t.Errorf("item = %+v, want product state populated", body.Data[0])
	}
}

func TestWishlistAddDuplicate(t *testing.T) {
	h, pool := newWishlistHandler(t)
	email := "wishlist-dup@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)

	prod := seedProduct(t, pool, "Produk Dup", 10000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, prod.ID)
	defer pool.Exec(context.Background(), `DELETE FROM wishlists WHERE user_id = $1`, userID)

	token := userToken(t, userID, "user")
	first := wishlistRequest(t, h, http.MethodPost, "", token, `{"product_id":"`+prod.ID+`"}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first add status = %d, want 201", first.Code)
	}

	rec := wishlistRequest(t, h, http.MethodPost, "", token, `{"product_id":"`+prod.ID+`"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate add status = %d, want 409, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "PRODUCT_ALREADY_IN_WISHLIST") {
		t.Errorf("body = %s, want PRODUCT_ALREADY_IN_WISHLIST", rec.Body.String())
	}
}

func TestWishlistAddProductNotFound(t *testing.T) {
	h, pool := newWishlistHandler(t)
	email := "wishlist-404@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)

	rec := wishlistRequest(t, h, http.MethodPost, "", userToken(t, userID, "user"),
		`{"product_id":"00000000-0000-0000-0000-000000000000"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "PRODUCT_NOT_FOUND") {
		t.Errorf("body = %s, want PRODUCT_NOT_FOUND", rec.Body.String())
	}
}

func TestWishlistAddSoftDeletedProduct(t *testing.T) {
	h, pool := newWishlistHandler(t)
	email := "wishlist-softdel@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)

	prod := seedProduct(t, pool, "Produk Dihapus", 10000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, prod.ID)

	repo := repository.NewProductRepo(pool)
	if err := repo.SoftDelete(context.Background(), prod.ID); err != nil {
		t.Fatal(err)
	}

	rec := wishlistRequest(t, h, http.MethodPost, "", userToken(t, userID, "user"), `{"product_id":"`+prod.ID+`"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (soft-deleted product not wishlistable)", rec.Code)
	}
}

func TestWishlistRemoveAndListExcludesSoftDeleted(t *testing.T) {
	h, pool := newWishlistHandler(t)
	email := "wishlist-rm@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)

	p1 := seedProduct(t, pool, "Produk A", 10000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, p1.ID)
	p2 := seedProduct(t, pool, "Produk B", 10000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, p2.ID)
	defer pool.Exec(context.Background(), `DELETE FROM wishlists WHERE user_id = $1`, userID)

	token := userToken(t, userID, "user")
	for _, pid := range []string{p1.ID, p2.ID} {
		if rec := wishlistRequest(t, h, http.MethodPost, "", token, `{"product_id":"`+pid+`"}`); rec.Code != http.StatusCreated {
			t.Fatalf("add %s status = %d", pid, rec.Code)
		}
	}

	// soft-delete p2 -> it must disappear from the list
	repo := repository.NewProductRepo(pool)
	if err := repo.SoftDelete(context.Background(), p2.ID); err != nil {
		t.Fatal(err)
	}

	rec := wishlistRequest(t, h, http.MethodGet, "", token, "")
	var body struct {
		Data []struct {
			ProductID string `json:"product_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, item := range body.Data {
		if item.ProductID == p2.ID {
			t.Error("soft-deleted product must be excluded from wishlist list")
		}
	}

	rec = wishlistRequest(t, h, http.MethodDelete, p1.ID, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("remove status = %d, want 200", rec.Code)
	}

	rec = wishlistRequest(t, h, http.MethodGet, "", token, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, item := range body.Data {
		if item.ProductID == p1.ID {
			t.Error("removed product must not appear in list")
		}
	}
}

func TestWishlistUnauthorized(t *testing.T) {
	h, _ := newWishlistHandler(t)
	rec := wishlistRequest(t, h, http.MethodGet, "", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
