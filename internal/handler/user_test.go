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

func newUserHandler(t *testing.T) (*User, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping users handler test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return NewUser(service.NewUserService(repository.NewUserRepo(pool))), pool
}

func userToken(t *testing.T, userID, role string) string {
	t.Helper()
	token, err := jwtpkg.New("test-secret", jwtpkg.DefaultTTL).Generate(userID, role)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func meRequest(t *testing.T, h *User, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/users/me", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))(http.HandlerFunc(h.Me)).ServeHTTP(rec, req)
	return rec
}

func TestMeUnauthorized(t *testing.T) {
	h, _ := newUserHandler(t)
	rec := meRequest(t, h, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "UNAUTHORIZED") {
		t.Errorf("body = %s, want UNAUTHORIZED", rec.Body.String())
	}
}

func TestMeSuccess(t *testing.T) {
	h, pool := newUserHandler(t)
	email := "me-success@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)

	rec := meRequest(t, h, userToken(t, userID, "user"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Email  string `json:"email"`
			Role   string `json:"role"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || body.Data.ID != userID || body.Data.Email != email || body.Data.Role != "user" || body.Data.Status != "active" {
		t.Errorf("body = %+v, want id=%s email=%s role=user status=active", body.Data, userID, email)
	}
	if strings.Contains(rec.Body.String(), "password_hash") || strings.Contains(rec.Body.String(), "password") {
		t.Error("response must not expose password_hash")
	}
}

func TestMeInvalidToken(t *testing.T) {
	h, _ := newUserHandler(t)
	rec := meRequest(t, h, "garbage-token")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func patchMe(t *testing.T, h *User, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/users/me", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))(http.HandlerFunc(h.UpdateMe)).ServeHTTP(rec, req)
	return rec
}

func TestUpdateMeSuccess(t *testing.T) {
	h, pool := newUserHandler(t)
	email := "me-update@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)

	rec := patchMe(t, h, userToken(t, userID, "user"), `{"name":"Budi Baru","phone":"08123456789"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var name, phone string
	pool.QueryRow(context.Background(), `SELECT name, coalesce(phone, '') FROM users WHERE id = $1`, userID).Scan(&name, &phone)
	if name != "Budi Baru" || phone != "08123456789" {
		t.Errorf("DB = name=%q phone=%q, want Budi Baru/08123456789", name, phone)
	}
}

func TestUpdateMeRoleIgnored(t *testing.T) {
	h, pool := newUserHandler(t)
	email := "me-role@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)

	rec := patchMe(t, h, userToken(t, userID, "user"), `{"name":"Budi","role":"admin"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var role string
	pool.QueryRow(context.Background(), `SELECT role FROM users WHERE id = $1`, userID).Scan(&role)
	if role != "user" {
		t.Errorf("role = %q, want user (role must be ignored from body)", role)
	}
}

func TestUpdateMeEmptyName(t *testing.T) {
	h, pool := newUserHandler(t)
	email := "me-empty@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)

	rec := patchMe(t, h, userToken(t, userID, "user"), `{"name":"","phone":"0812"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "VALIDATION_ERROR") {
		t.Errorf("body = %s, want VALIDATION_ERROR", rec.Body.String())
	}
}

func TestUpdateMeUnauthorized(t *testing.T) {
	h, _ := newUserHandler(t)
	rec := patchMe(t, h, "", `{"name":"Budi"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func seedAdmin(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()
	seedUser(t, pool, email, "abc12345", "active")
	var id string
	err := pool.QueryRow(context.Background(),
		`UPDATE users SET role = 'admin' WHERE email = $1 RETURNING id`, email).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func adminListRequest(t *testing.T, h *User, token, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/users"+query, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	auth := middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))
	auth(middleware.RequireRole("admin")(http.HandlerFunc(h.ListUsers))).ServeHTTP(rec, req)
	return rec
}

func adminStatusRequest(t *testing.T, h *User, token, userID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/admin/users/"+userID+"/status", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.SetPathValue("id", userID)
	rec := httptest.NewRecorder()
	auth := middleware.RequireAuth(jwtpkg.New("test-secret", jwtpkg.DefaultTTL))
	auth(middleware.RequireRole("admin")(http.HandlerFunc(h.UpdateUserStatus))).ServeHTTP(rec, req)
	return rec
}

func TestAdminListUsersUnauthorized(t *testing.T) {
	h, _ := newUserHandler(t)
	rec := adminListRequest(t, h, "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAdminListUsersForbidden(t *testing.T) {
	h, pool := newUserHandler(t)
	email := "admin-list-forbidden@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)

	rec := adminListRequest(t, h, userToken(t, userID, "user"), "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestAdminListUsersSuccess(t *testing.T) {
	h, pool := newUserHandler(t)
	adminEmail := "admin-list-success@example.com"
	userEmail := "admin-list-user@example.com"
	adminID := seedAdmin(t, pool, adminEmail)
	seedUser(t, pool, userEmail, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = ANY($1)`, []string{adminEmail, userEmail})

	rec := adminListRequest(t, h, userToken(t, adminID, "admin"), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Success bool `json:"success"`
		Data    []struct {
			Email  string `json:"email"`
			Status string `json:"status"`
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
	if !body.Success || body.Meta.Page != 1 || body.Meta.Limit != 12 || body.Meta.Total < 2 {
		t.Errorf("body = %+v, want page=1 limit=12 total>=2", body)
	}
	if strings.Contains(rec.Body.String(), "password_hash") {
		t.Error("response must not expose password_hash")
	}
	found := map[string]bool{}
	for _, u := range body.Data {
		found[u.Email] = true
	}
	if !found[userEmail] {
		t.Errorf("list must contain %s, got %+v", userEmail, found)
	}
}

func TestAdminListUsersFilterStatus(t *testing.T) {
	h, pool := newUserHandler(t)
	adminEmail := "admin-list-filter@example.com"
	inactiveEmail := "admin-list-inactive@example.com"
	adminID := seedAdmin(t, pool, adminEmail)
	seedUser(t, pool, inactiveEmail, "abc12345", "inactive")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = ANY($1)`, []string{adminEmail, inactiveEmail})

	rec := adminListRequest(t, h, userToken(t, adminID, "admin"), "?status=inactive")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Data []struct {
			Email  string `json:"email"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 || body.Data[0].Email != inactiveEmail {
		t.Errorf("data = %+v, want exactly the inactive user", body.Data)
	}
}

func TestAdminListUsersInvalidStatus(t *testing.T) {
	h, pool := newUserHandler(t)
	adminID := seedAdmin(t, pool, "admin-list-invalid@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-list-invalid@example.com")

	rec := adminListRequest(t, h, userToken(t, adminID, "admin"), "?status=banned")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "INVALID_QUERY_PARAM") {
		t.Errorf("body = %s, want INVALID_QUERY_PARAM", rec.Body.String())
	}
}

func TestAdminUpdateUserStatusSuccess(t *testing.T) {
	h, pool := newUserHandler(t)
	adminEmail := "admin-status-success@example.com"
	targetEmail := "admin-status-target@example.com"
	adminID := seedAdmin(t, pool, adminEmail)
	seedUser(t, pool, targetEmail, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = ANY($1)`, []string{adminEmail, targetEmail})

	var targetID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, targetEmail).Scan(&targetID)

	rec := adminStatusRequest(t, h, userToken(t, adminID, "admin"), targetID, `{"status":"inactive"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var status string
	pool.QueryRow(context.Background(), `SELECT status FROM users WHERE id = $1`, targetID).Scan(&status)
	if status != "inactive" {
		t.Errorf("status = %q, want inactive", status)
	}
}

func TestAdminUpdateUserStatusInvalidStatus(t *testing.T) {
	h, pool := newUserHandler(t)
	adminEmail := "admin-status-invalid@example.com"
	targetEmail := "admin-status-invalid-target@example.com"
	adminID := seedAdmin(t, pool, adminEmail)
	seedUser(t, pool, targetEmail, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = ANY($1)`, []string{adminEmail, targetEmail})

	var targetID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, targetEmail).Scan(&targetID)

	rec := adminStatusRequest(t, h, userToken(t, adminID, "admin"), targetID, `{"status":"deleted"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "VALIDATION_ERROR") {
		t.Errorf("body = %s, want VALIDATION_ERROR", rec.Body.String())
	}
}

func TestAdminUpdateUserStatusNotFound(t *testing.T) {
	h, pool := newUserHandler(t)
	adminID := seedAdmin(t, pool, "admin-status-notfound@example.com")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, "admin-status-notfound@example.com")

	rec := adminStatusRequest(t, h, userToken(t, adminID, "admin"), "00000000-0000-0000-0000-000000000000", `{"status":"inactive"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "USER_NOT_FOUND") {
		t.Errorf("body = %s, want USER_NOT_FOUND", rec.Body.String())
	}
}

func TestAdminUpdateUserStatusForbidden(t *testing.T) {
	h, pool := newUserHandler(t)
	adminEmail := "admin-status-forbidden@example.com"
	seedUser(t, pool, adminEmail, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, adminEmail)

	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, adminEmail).Scan(&userID)

	rec := adminStatusRequest(t, h, userToken(t, userID, "user"), userID, `{"status":"inactive"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
