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
	"ecommerce/server/internal/model"
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

func listProductsRequest(t *testing.T, h *Product, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/products"+query, nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	return rec
}

func seedProduct(t *testing.T, pool *pgxpool.Pool, name string, price, stock int64, categoryID *string) *model.Product {
	t.Helper()
	repo := repository.NewProductRepo(pool)
	p, err := repo.Create(context.Background(), name, nil, price, stock, categoryID)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProductListPublic(t *testing.T) {
	h, pool := newProductHandler(t)
	catID := seedCategory(t, pool, "Pakaian", "pakaian")
	defer pool.Exec(context.Background(), `DELETE FROM categories WHERE id = $1`, catID)

	cat := &catID
	p1 := seedProduct(t, pool, "Kaos Polos", 89000, 10, cat)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, p1.ID)
	p2 := seedProduct(t, pool, "Celana Jeans", 150000, 0, cat)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, p2.ID)

	rec := listProductsRequest(t, h, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Success bool `json:"success"`
		Data    []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Price      int64  `json:"price"`
			Stock      int64  `json:"stock"`
			IsActive   bool   `json:"is_active"`
			PrimaryImg any    `json:"primary_image"`
			Category   struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"category"`
		} `json:"data"`
		Meta struct {
			Page       int `json:"page"`
			Limit      int `json:"limit"`
			Total      int `json:"total"`
			TotalPages int `json:"total_pages"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || body.Meta.Limit != 12 || body.Meta.Total < 2 {
		t.Errorf("body = %+v, want success + limit 12 + total>=2", body)
	}
	found1, found2 := false, false
	for _, item := range body.Data {
		if item.ID == p1.ID {
			found1 = true
			if item.Name != "Kaos Polos" || item.Price != 89000 || item.Category.ID != catID || item.Category.Name != "Pakaian" {
				t.Errorf("item = %+v, want name/price/category populated", item)
			}
		}
		if item.ID == p2.ID {
			found2 = true
		}
	}
	if !found1 || !found2 {
		t.Errorf("list must contain both seeded products, got %+v", body.Data)
	}
}

func TestProductListFilterSearch(t *testing.T) {
	h, pool := newProductHandler(t)
	p1 := seedProduct(t, pool, "Kaos Polos", 89000, 10, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, p1.ID)
	seedProduct(t, pool, "Celana Jeans", 150000, 5, nil)

	rec := listProductsRequest(t, h, "?search=kaos")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 || body.Data[0].Name != "Kaos Polos" || body.Meta.Total != 1 {
		t.Errorf("search filter = %+v, want only Kaos Polos", body)
	}
}

func TestProductListFilterPriceRange(t *testing.T) {
	h, pool := newProductHandler(t)
	low := seedProduct(t, pool, "Murah", 5000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, low.ID)
	mid := seedProduct(t, pool, "Sedang", 100000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, mid.ID)
	high := seedProduct(t, pool, "Mahal", 500000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, high.ID)

	rec := listProductsRequest(t, h, "?min_price=10000&max_price=200000")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, item := range body.Data {
		got[item.ID] = true
	}
	if !got[mid.ID] {
		t.Error("mid product (100000) must be in price range 10000-200000")
	}
	if got[low.ID] || got[high.ID] {
		t.Errorf("price range filter wrong: low=%v high=%v", got[low.ID], got[high.ID])
	}
}

func TestProductListSortPriceAsc(t *testing.T) {
	h, pool := newProductHandler(t)
	p1 := seedProduct(t, pool, "Murah", 5000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, p1.ID)
	p2 := seedProduct(t, pool, "Mahal", 500000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, p2.ID)

	rec := listProductsRequest(t, h, "?sort=price_asc")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Data []struct {
			ID    string `json:"id"`
			Price int64  `json:"price"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) < 2 {
		t.Fatalf("expected at least 2 items, got %d", len(body.Data))
	}
	// first item must be the cheapest; verify it's one of ours and cheapest
	if body.Data[0].Price != 5000 {
		t.Errorf("first price = %d, want 5000 (cheapest first)", body.Data[0].Price)
	}
}

func TestProductListSortInvalid(t *testing.T) {
	h, _ := newProductHandler(t)
	rec := listProductsRequest(t, h, "?sort=bogus")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "INVALID_QUERY_PARAM") {
		t.Errorf("body = %s, want INVALID_QUERY_PARAM", rec.Body.String())
	}
}

func TestProductListInvalidMinPrice(t *testing.T) {
	h, _ := newProductHandler(t)
	rec := listProductsRequest(t, h, "?min_price=abc")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "INVALID_QUERY_PARAM") {
		t.Errorf("body = %s, want INVALID_QUERY_PARAM", rec.Body.String())
	}
}

func TestProductListInStockFilter(t *testing.T) {
	h, pool := newProductHandler(t)
	p1 := seedProduct(t, pool, "Ada Stock", 10000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, p1.ID)
	p2 := seedProduct(t, pool, "Habis", 10000, 0, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, p2.ID)

	rec := listProductsRequest(t, h, "?in_stock=true")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, item := range body.Data {
		if item.ID == p2.ID {
			t.Error("out-of-stock product must not appear with in_stock=true")
		}
	}
}

func TestProductListSoftDeletedHidden(t *testing.T) {
	h, pool := newProductHandler(t)
	p := seedProduct(t, pool, "Dihapus", 10000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, p.ID)

	repo := repository.NewProductRepo(pool)
	if err := repo.SoftDelete(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}

	rec := listProductsRequest(t, h, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), p.ID) {
		t.Errorf("soft-deleted product must not appear in public listing: %s", rec.Body.String())
	}
}

func TestProductListPagination(t *testing.T) {
	h, pool := newProductHandler(t)
	ids := []string{}
	defer func() {
		pool.Exec(context.Background(), `DELETE FROM products WHERE id = ANY($1)`, ids)
	}()
	for i := 0; i < 5; i++ {
		p := seedProduct(t, pool, "Produk"+string(rune('A'+i)), 10000, 5, nil)
		ids = append(ids, p.ID)
	}

	rec := listProductsRequest(t, h, "?limit=2&page=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Meta struct {
			Page       int `json:"page"`
			Limit      int `json:"limit"`
			Total      int `json:"total"`
			TotalPages int `json:"total_pages"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 2 || body.Meta.Limit != 2 || body.Meta.Total < 5 || body.Meta.TotalPages < 3 {
		t.Errorf("pagination = %+v, want 2 per page, total>=5, pages>=3", body.Meta)
	}
}

func seedProductImage(t *testing.T, pool *pgxpool.Pool, productID, url string, primary bool, order int) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(),
		`INSERT INTO product_images (product_id, url, is_primary, display_order)
		 VALUES ($1, $2, $3, $4) RETURNING id`, productID, url, primary, order).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func productDetailRequest(t *testing.T, h *Product, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/products/"+id, nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	h.Detail(rec, req)
	return rec
}

func TestProductDetailSuccess(t *testing.T) {
	h, pool := newProductHandler(t)
	catID := seedCategory(t, pool, "Pakaian", "pakaian")
	defer pool.Exec(context.Background(), `DELETE FROM categories WHERE id = $1`, catID)

	cat := &catID
	prod := seedProduct(t, pool, "Kaos Detail", 89000, 15, cat)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, prod.ID)

	img1 := seedProductImage(t, pool, prod.ID, "https://img.example.com/1.jpg", true, 0)
	img2 := seedProductImage(t, pool, prod.ID, "https://img.example.com/2.jpg", false, 1)
	defer pool.Exec(context.Background(), `DELETE FROM product_images WHERE product_id = $1`, prod.ID)

	rec := productDetailRequest(t, h, prod.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Price    int64  `json:"price"`
			Stock    int64  `json:"stock"`
			InStock  bool   `json:"in_stock"`
			Category struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"category"`
			Images []struct {
				ID      string `json:"id"`
				URL     string `json:"url"`
				Primary bool   `json:"is_primary"`
			} `json:"images"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || body.Data.ID != prod.ID || body.Data.Name != "Kaos Detail" || body.Data.Price != 89000 {
		t.Errorf("body = %+v, want product detail", body.Data)
	}
	if body.Data.Stock != 15 || !body.Data.InStock {
		t.Errorf("stock = %d in_stock=%v, want 15/true", body.Data.Stock, body.Data.InStock)
	}
	if body.Data.Category.ID != catID || body.Data.Category.Name != "Pakaian" {
		t.Errorf("category = %+v, want Pakaian", body.Data.Category)
	}
	if len(body.Data.Images) != 2 {
		t.Fatalf("images = %+v, want 2", body.Data.Images)
	}
	if body.Data.Images[0].ID != img1 || !body.Data.Images[0].Primary || body.Data.Images[0].URL != "https://img.example.com/1.jpg" {
		t.Errorf("images[0] = %+v, want primary first", body.Data.Images[0])
	}
	if body.Data.Images[1].ID != img2 {
		t.Errorf("images[1] = %+v, want second image", body.Data.Images[1])
	}
}

func TestProductDetailNotFound(t *testing.T) {
	h, _ := newProductHandler(t)
	rec := productDetailRequest(t, h, "00000000-0000-0000-0000-000000000000")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "PRODUCT_NOT_FOUND") {
		t.Errorf("body = %s, want PRODUCT_NOT_FOUND", rec.Body.String())
	}
}

func TestProductDetailInactiveHidden(t *testing.T) {
	h, pool := newProductHandler(t)
	prod := seedProduct(t, pool, "Nonaktif", 10000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, prod.ID)

	repo := repository.NewProductRepo(pool)
	if _, err := repo.SetActive(context.Background(), prod.ID, false); err != nil {
		t.Fatal(err)
	}

	rec := productDetailRequest(t, h, prod.ID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (inactive product not public)", rec.Code)
	}
}

func TestProductDetailSoftDeletedHidden(t *testing.T) {
	h, pool := newProductHandler(t)
	prod := seedProduct(t, pool, "Dihapus Detail", 10000, 5, nil)
	defer pool.Exec(context.Background(), `DELETE FROM products WHERE id = $1`, prod.ID)

	repo := repository.NewProductRepo(pool)
	if err := repo.SoftDelete(context.Background(), prod.ID); err != nil {
		t.Fatal(err)
	}

	rec := productDetailRequest(t, h, prod.ID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (soft-deleted product not public)", rec.Code)
	}
}
