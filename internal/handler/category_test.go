package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// catTestName returns a unique category name so its auto-generated slug never
// collides with dev/leftover data in the shared DB.
func catTestName(base string) string {
	return fmt.Sprintf("%s %d", base, time.Now().UnixNano())
}

// slugifyForTest mirrors the service slugify for the "Base <digits>" shape
// produced by catTestName (lowercase, spaces -> dashes).
func slugifyForTest(name string) string {
	return strings.ReplaceAll(strings.ToLower(name), " ", "-")
}

func newCategoryHandler(t *testing.T) (*Category, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping category handler test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return NewCategory(service.NewCategoryService(repository.NewCategoryRepo(pool))), pool
}

func adminCategoryRequest(t *testing.T, h *Category, method, id, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/admin/categories"
	if id != "" {
		path += "/" + id
	}
	var reqBody *bytes.Buffer
	if body == "" {
		reqBody = bytes.NewBufferString("{}")
	} else {
		reqBody = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, reqBody)
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

func TestCategoryCreateSuccess(t *testing.T) {
	h, pool := newCategoryHandler(t)
	adminID := seedAdmin(t, pool, "admin-cat-create@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-cat-create@example.com")

	name := catTestName("Pakaian")
	rec := adminCategoryRequest(t, h, http.MethodPost, "", userToken(t, adminID, "admin"), `{"name":"`+name+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || body.Data.Name != name || body.Data.Slug != slugifyForTest(name) || body.Data.ID == "" {
		t.Errorf("body = %+v, want name=%s slug=%s", body, name, slugifyForTest(name))
	}
	defer pool.Exec(context.Background(), `DELETE FROM categories WHERE id = $1`, body.Data.ID)
}

func TestCategoryCreateUnauthorized(t *testing.T) {
	h, _ := newCategoryHandler(t)
	rec := adminCategoryRequest(t, h, http.MethodPost, "", "", `{"name":"Pakaian"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestCategoryCreateForbidden(t *testing.T) {
	h, pool := newCategoryHandler(t)
	email := "user-cat-create@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)

	rec := adminCategoryRequest(t, h, http.MethodPost, "", userToken(t, userID, "user"), `{"name":"Pakaian"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestCategoryCreateDuplicateSlug(t *testing.T) {
	h, pool := newCategoryHandler(t)
	adminID := seedAdmin(t, pool, "admin-cat-dup@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-cat-dup@example.com")

	token := userToken(t, adminID, "admin")
	name := catTestName("Aksesoris")
	first := adminCategoryRequest(t, h, http.MethodPost, "", token, `{"name":"`+name+`"}`)
	if first.Code != http.StatusCreated {
		t.Fatalf("first create status = %d, body: %s", first.Code, first.Body.String())
	}
	var parsed struct {
		Data struct{ ID string } `json:"data"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM categories WHERE id = $1`, parsed.Data.ID)

	// same slug via same name
	rec := adminCategoryRequest(t, h, http.MethodPost, "", token, `{"name":"`+name+`"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "CATEGORY_SLUG_EXISTS") {
		t.Errorf("body = %s, want CATEGORY_SLUG_EXISTS", rec.Body.String())
	}
}

func TestCategoryCreateEmptyName(t *testing.T) {
	h, pool := newCategoryHandler(t)
	adminID := seedAdmin(t, pool, "admin-cat-empty@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-cat-empty@example.com")

	rec := adminCategoryRequest(t, h, http.MethodPost, "", userToken(t, adminID, "admin"), `{"name":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "VALIDATION_ERROR") {
		t.Errorf("body = %s, want VALIDATION_ERROR", rec.Body.String())
	}
}

func TestCategoryUpdate(t *testing.T) {
	h, pool := newCategoryHandler(t)
	adminID := seedAdmin(t, pool, "admin-cat-update@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-cat-update@example.com")

	catRepo := repository.NewCategoryRepo(pool)
	origName := catTestName("Pakaian")
	cat, err := catRepo.Create(context.Background(), origName, slugifyForTest(origName))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM categories WHERE id = $1`, cat.ID)

	newName := catTestName("Pakaian Pria")
	rec := adminCategoryRequest(t, h, http.MethodPut, cat.ID, userToken(t, adminID, "admin"), `{"name":"`+newName+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Data struct {
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Name != newName || body.Data.Slug != slugifyForTest(newName) {
		t.Errorf("body = %+v, want name=%s slug=%s", body.Data, newName, slugifyForTest(newName))
	}
}

func TestCategoryUpdateNotFound(t *testing.T) {
	h, pool := newCategoryHandler(t)
	adminID := seedAdmin(t, pool, "admin-cat-upd404@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-cat-upd404@example.com")

	rec := adminCategoryRequest(t, h, http.MethodPut, "00000000-0000-0000-0000-000000000000", userToken(t, adminID, "admin"), `{"name":"Pakaian"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "CATEGORY_NOT_FOUND") {
		t.Errorf("body = %s, want CATEGORY_NOT_FOUND", rec.Body.String())
	}
}

func TestCategoryDeleteSoftAndHiddenFromPublic(t *testing.T) {
	h, pool := newCategoryHandler(t)
	adminID := seedAdmin(t, pool, "admin-cat-del@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-cat-del@example.com")

	catRepo := repository.NewCategoryRepo(pool)
	delName := catTestName("Elektronik")
	cat, err := catRepo.Create(context.Background(), delName, slugifyForTest(delName))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM categories WHERE id = $1`, cat.ID)

	rec := adminCategoryRequest(t, h, http.MethodDelete, cat.ID, userToken(t, adminID, "admin"), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	// still in DB (soft delete), but gone from public listing
	var deletedAtIsNull bool
	pool.QueryRow(context.Background(), `SELECT deleted_at IS NULL FROM categories WHERE id = $1`, cat.ID).Scan(&deletedAtIsNull)
	if deletedAtIsNull {
		t.Error("category row must still exist with deleted_at set (soft delete)")
	}

	publicRec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/categories", nil)
	h.ListActive(publicRec, req)
	if publicRec.Code != http.StatusOK {
		t.Fatalf("public status = %d, want 200", publicRec.Code)
	}
	if strings.Contains(publicRec.Body.String(), cat.ID) {
		t.Errorf("public list still contains deleted category: %s", publicRec.Body.String())
	}
}

func TestCategoryListActivePublic(t *testing.T) {
	h, pool := newCategoryHandler(t)
	catRepo := repository.NewCategoryRepo(pool)
	bukuName := catTestName("Buku")
	cat, err := catRepo.Create(context.Background(), bukuName, slugifyForTest(bukuName))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM categories WHERE id = $1`, cat.ID)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/categories", nil)
	h.ListActive(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Success bool `json:"success"`
		Data    []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success {
		t.Error("success must be true")
	}
	found := false
	for _, c := range body.Data {
		if c.ID == cat.ID && c.Name == bukuName && c.Slug == slugifyForTest(bukuName) {
			found = true
		}
	}
	if !found {
		t.Errorf("public list must contain the active category, got %+v", body.Data)
	}
}
