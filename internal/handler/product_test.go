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

func newProductHandler(t *testing.T) (*Product, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping product handler test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return NewProduct(service.NewProductService(repository.NewProductRepo(pool))), pool
}

func adminProductRequest(t *testing.T, h *Product, method, id, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/admin/products"
	if id != "" {
		path += "/" + id
	}
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if id != "" {
		req.SetPathValue("id", id)
	}
	rec := httptest.NewRecorder()
	auth := middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))
	var hf func(http.ResponseWriter, *http.Request)
	switch method {
	case http.MethodPut:
		hf = h.Update
	case http.MethodDelete:
		hf = h.Delete
	default:
		hf = h.Create
	}
	auth(middleware.RequireRole("admin")(http.HandlerFunc(hf))).ServeHTTP(rec, req)
	return rec
}

func adminProductSubRequest(t *testing.T, h *Product, sub, id, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/admin/products/"+id+"/"+sub, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	auth := middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))
	var hf func(http.ResponseWriter, *http.Request)
	switch sub {
	case "stock":
		hf = h.UpdateStock
	default:
		hf = h.UpdateStatus
	}
	auth(middleware.RequireRole("admin")(http.HandlerFunc(hf))).ServeHTTP(rec, req)
	return rec
}

func seedCategory(t *testing.T, pool *pgxpool.Pool, name, slug string) string {
	t.Helper()
	repo := repository.NewCategoryRepo(pool)
	cat, err := repo.Create(context.Background(), name, slug)
	if err != nil {
		t.Fatal(err)
	}
	return cat.ID
}

func TestProductCreateSuccess(t *testing.T) {
	h, pool := newProductHandler(t)
	adminID := seedAdmin(t, pool, "admin-prod-create@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-prod-create@example.com")

	catID := seedCategory(t, pool, "Pakaian", "pakaian")
	defer pool.Exec(context.Background(), `DELETE FROM categories WHERE id = $1`, catID)

	rec := adminProductRequest(t, h, http.MethodPost, "", userToken(t, adminID, "admin"),
		`{"name":"Kaos Polos","description":"Katun combed","price":89000,"stock":50,"category_id":"`+catID+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			ID          string  `json:"id"`
			Name        string  `json:"name"`
			Price       int64   `json:"price"`
			Stock       int64   `json:"stock"`
			IsActive    bool    `json:"is_active"`
			CategoryID  *string `json:"category_id"`
			Description *string `json:"description"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || body.Data.Name != "Kaos Polos" || body.Data.Price != 89000 || body.Data.Stock != 50 {
		t.Errorf("body = %+v, want created product", body.Data)
	}
	if !body.Data.IsActive {
		t.Error("new product must be active by default")
	}
	if body.Data.CategoryID == nil || *body.Data.CategoryID != catID {
		t.Errorf("category_id = %v, want %s", body.Data.CategoryID, catID)
	}
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, body.Data.ID)
}

func TestProductCreateValidation(t *testing.T) {
	h, pool := newProductHandler(t)
	adminID := seedAdmin(t, pool, "admin-prod-val@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-prod-val@example.com")

	rec := adminProductRequest(t, h, http.MethodPost, "", userToken(t, adminID, "admin"), `{"name":"","price":-1,"stock":-5}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "VALIDATION_ERROR") {
		t.Errorf("body = %s, want VALIDATION_ERROR", rec.Body.String())
	}
}

func TestProductCreateCategoryNotFound(t *testing.T) {
	h, pool := newProductHandler(t)
	adminID := seedAdmin(t, pool, "admin-prod-cat404@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-prod-cat404@example.com")

	rec := adminProductRequest(t, h, http.MethodPost, "", userToken(t, adminID, "admin"),
		`{"name":"Kaos","price":1000,"stock":1,"category_id":"00000000-0000-0000-0000-000000000000"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "CATEGORY_NOT_FOUND") {
		t.Errorf("body = %s, want CATEGORY_NOT_FOUND", rec.Body.String())
	}
}

func TestProductCreateUnauthorized(t *testing.T) {
	h, _ := newProductHandler(t)
	rec := adminProductRequest(t, h, http.MethodPost, "", "", `{"name":"Kaos","price":1000,"stock":1}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestProductCreateForbidden(t *testing.T) {
	h, pool := newProductHandler(t)
	email := "user-prod-create@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)

	rec := adminProductRequest(t, h, http.MethodPost, "", userToken(t, userID, "user"), `{"name":"Kaos","price":1000,"stock":1}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestProductUpdate(t *testing.T) {
	h, pool := newProductHandler(t)
	adminID := seedAdmin(t, pool, "admin-prod-upd@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-prod-upd@example.com")

	prodRepo := repository.NewProductRepo(pool)
	created, err := prodRepo.Create(context.Background(), "Kaos Lama", nil, 89000, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, created.ID)

	rec := adminProductRequest(t, h, http.MethodPut, created.ID, userToken(t, adminID, "admin"),
		`{"name":"Kaos Baru","description":"Upgraded","price":99000,"stock":20}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Data struct {
			Name        string  `json:"name"`
			Price       int64   `json:"price"`
			Stock       int64   `json:"stock"`
			Description *string `json:"description"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Name != "Kaos Baru" || body.Data.Price != 99000 || body.Data.Stock != 20 || body.Data.Description == nil || *body.Data.Description != "Upgraded" {
		t.Errorf("body = %+v, want updated product", body.Data)
	}
}

func TestProductUpdateNotFound(t *testing.T) {
	h, pool := newProductHandler(t)
	adminID := seedAdmin(t, pool, "admin-prod-upd404@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-prod-upd404@example.com")

	rec := adminProductRequest(t, h, http.MethodPut, "00000000-0000-0000-0000-000000000000", userToken(t, adminID, "admin"),
		`{"name":"Kaos","price":1000,"stock":1}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "PRODUCT_NOT_FOUND") {
		t.Errorf("body = %s, want PRODUCT_NOT_FOUND", rec.Body.String())
	}
}

func TestProductDeleteSoftAndHiddenFromActive(t *testing.T) {
	h, pool := newProductHandler(t)
	adminID := seedAdmin(t, pool, "admin-prod-del@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-prod-del@example.com")

	prodRepo := repository.NewProductRepo(pool)
	created, err := prodRepo.Create(context.Background(), "Sepatu", nil, 250000, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, created.ID)

	rec := adminProductRequest(t, h, http.MethodDelete, created.ID, userToken(t, adminID, "admin"), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var deletedAtIsNull bool
	pool.QueryRow(context.Background(), `SELECT deleted_at IS NULL FROM products WHERE id = $1`, created.ID).Scan(&deletedAtIsNull)
	if deletedAtIsNull {
		t.Error("product row must still exist with deleted_at set (soft delete)")
	}

	list, _, err := prodRepo.ListActive(context.Background(), 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list {
		if item.ID == created.ID {
			t.Error("ListActive() still contains soft-deleted product")
		}
	}
}

func TestProductDeleteNotFound(t *testing.T) {
	h, pool := newProductHandler(t)
	adminID := seedAdmin(t, pool, "admin-prod-del404@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-prod-del404@example.com")

	rec := adminProductRequest(t, h, http.MethodDelete, "00000000-0000-0000-0000-000000000000", userToken(t, adminID, "admin"), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "PRODUCT_NOT_FOUND") {
		t.Errorf("body = %s, want PRODUCT_NOT_FOUND", rec.Body.String())
	}
}

func TestProductUpdateStatus(t *testing.T) {
	h, pool := newProductHandler(t)
	adminID := seedAdmin(t, pool, "admin-prod-status@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-prod-status@example.com")

	prodRepo := repository.NewProductRepo(pool)
	created, err := prodRepo.Create(context.Background(), "Produk", nil, 10000, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, created.ID)

	rec := adminProductSubRequest(t, h, "status", created.ID, userToken(t, adminID, "admin"), `{"is_active":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Data struct {
			IsActive bool `json:"is_active"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.IsActive {
		t.Error("is_active must be false after toggle")
	}

	list, _, err := prodRepo.ListActive(context.Background(), 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list {
		if item.ID == created.ID {
			t.Error("inactive product still appears in ListActive()")
		}
	}
}

func TestProductUpdateStock(t *testing.T) {
	h, pool := newProductHandler(t)
	adminID := seedAdmin(t, pool, "admin-prod-stock@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-prod-stock@example.com")

	prodRepo := repository.NewProductRepo(pool)
	created, err := prodRepo.Create(context.Background(), "Produk", nil, 10000, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, created.ID)

	rec := adminProductSubRequest(t, h, "stock", created.ID, userToken(t, adminID, "admin"), `{"stock":25}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Data struct {
			Stock int `json:"stock"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Stock != 25 {
		t.Errorf("stock = %d, want 25", body.Data.Stock)
	}
}

func TestProductUpdateStockNegative(t *testing.T) {
	h, pool := newProductHandler(t)
	adminID := seedAdmin(t, pool, "admin-prod-stockneg@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-prod-stockneg@example.com")

	prodRepo := repository.NewProductRepo(pool)
	created, err := prodRepo.Create(context.Background(), "Produk", nil, 10000, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, created.ID)

	rec := adminProductSubRequest(t, h, "stock", created.ID, userToken(t, adminID, "admin"), `{"stock":-1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "VALIDATION_ERROR") {
		t.Errorf("body = %s, want VALIDATION_ERROR", rec.Body.String())
	}
}

func TestProductUpdateStatusNotFound(t *testing.T) {
	h, pool := newProductHandler(t)
	adminID := seedAdmin(t, pool, "admin-prod-status404@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-prod-status404@example.com")

	rec := adminProductSubRequest(t, h, "status", "00000000-0000-0000-0000-000000000000", userToken(t, adminID, "admin"), `{"is_active":true}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "PRODUCT_NOT_FOUND") {
		t.Errorf("body = %s, want PRODUCT_NOT_FOUND", rec.Body.String())
	}
}

func TestProductUpdateStockForbidden(t *testing.T) {
	h, pool := newProductHandler(t)
	email := "user-prod-stock@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)

	rec := adminProductSubRequest(t, h, "stock", "00000000-0000-0000-0000-000000000000", userToken(t, userID, "user"), `{"stock":1}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
