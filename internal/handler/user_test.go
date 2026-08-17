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
	return NewUser(repository.NewUserRepo(pool)), pool
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
