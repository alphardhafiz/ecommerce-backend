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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	jwtpkg "ecommerce/server/internal/jwt"
	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/service"
)

func newAuth(t *testing.T) (*Auth, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping auth handler test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	jwtHelper := jwtpkg.New("test-secret", jwtpkg.DefaultTTL)
	return NewAuth(service.NewAuthService(repository.NewUserRepo(pool), repository.NewRefreshTokenRepo(pool), jwtHelper)), pool
}

func postRegister(t *testing.T, h *Auth, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Register(rec, req)
	return rec
}

func TestRegisterSuccess(t *testing.T) {
	h, pool := newAuth(t)
	email := "register-success@example.com"
	pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	rec := postRegister(t, h, `{"name":"Budi","email":"`+email+`","password":"abc12345","confirm_password":"abc12345"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || body.Data.ID == "" || body.Data.Role != "user" {
		t.Errorf("body = %+v, want success + id + role=user", body)
	}

	var hash string
	err := pool.QueryRow(context.Background(), `SELECT password_hash FROM users WHERE email = $1`, email).Scan(&hash)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$2a$12$") {
		t.Errorf("password_hash = %q, want bcrypt cost 12", hash)
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	h, pool := newAuth(t)
	email := "register-dup@example.com"
	pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	body := `{"name":"Budi","email":"` + email + `","password":"abc12345","confirm_password":"abc12345"}`
	if rec := postRegister(t, h, body); rec.Code != http.StatusCreated {
		t.Fatalf("first register status = %d, want 201", rec.Code)
	}

	rec := postRegister(t, h, body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second register status = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "EMAIL_ALREADY_EXISTS") {
		t.Errorf("body = %s, want EMAIL_ALREADY_EXISTS", rec.Body.String())
	}
}

func TestRegisterValidation(t *testing.T) {
	h, _ := newAuth(t)

	tests := []struct {
		name string
		body string
	}{
		{"bad email", `{"name":"Budi","email":"x","password":"abc12345","confirm_password":"abc12345"}`},
		{"short password", `{"name":"Budi","email":"a@b.com","password":"ab1","confirm_password":"ab1"}`},
		{"mismatch", `{"name":"Budi","email":"a@b.com","password":"abc12345","confirm_password":"abc12346"}`},
		{"empty name", `{"name":"","email":"a@b.com","password":"abc12345","confirm_password":"abc12345"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := postRegister(t, h, tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "VALIDATION_ERROR") {
				t.Errorf("body = %s, want VALIDATION_ERROR", rec.Body.String())
			}
		})
	}
}

func postLogin(t *testing.T, h *Auth, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Login(rec, req)
	return rec
}

func seedUser(t *testing.T, pool *pgxpool.Pool, email, password, status string) {
	t.Helper()
	ctx := context.Background()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO users (name, email, password_hash, status) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (email) DO UPDATE SET password_hash = EXCLUDED.password_hash, status = EXCLUDED.status`,
		"Budi", email, string(hash), status)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoginSuccess(t *testing.T) {
	h, pool := newAuth(t)
	email := "login-success@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)
	defer pool.Exec(context.Background(), `DELETE FROM refresh_tokens WHERE user_id = (SELECT id FROM users WHERE email = $1)`, email)

	rec := postLogin(t, h, `{"email":"`+email+`","password":"abc12345"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int    `json:"expires_in"`
			User        struct {
				ID   string `json:"id"`
				Role string `json:"role"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || body.Data.AccessToken == "" || body.Data.ExpiresIn != 900 || body.Data.User.Role != "user" {
		t.Errorf("body = %+v, want success + access_token + expires_in=900 + role=user", body)
	}

	cookies := rec.Result().Cookies()
	var refreshCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "refresh_token" {
			refreshCookie = c
		}
	}
	if refreshCookie == nil {
		t.Fatal("refresh_token cookie not set")
	}
	if !refreshCookie.HttpOnly || !refreshCookie.Secure || refreshCookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie flags: HttpOnly=%v Secure=%v SameSite=%v, want all set", refreshCookie.HttpOnly, refreshCookie.Secure, refreshCookie.SameSite)
	}

	var storedHash string
	var expiresAt time.Time
	err := pool.QueryRow(context.Background(),
		`SELECT token_hash, expires_at FROM refresh_tokens rt JOIN users u ON u.id = rt.user_id WHERE u.email = $1 AND rt.revoked_at IS NULL`,
		email).Scan(&storedHash, &expiresAt)
	if err != nil {
		t.Fatalf("refresh token not stored in DB: %v", err)
	}
	if storedHash == "" || strings.Contains(storedHash, refreshCookie.Value) {
		t.Errorf("storedHash = %q, want SHA-256 hash, not raw token", storedHash)
	}
	ttl := time.Until(expiresAt)
	if ttl < 6*24*time.Hour || ttl > 7*24*time.Hour {
		t.Errorf("expires_at = %v, want ~7 days from now", ttl)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	h, pool := newAuth(t)
	email := "login-wrongpass@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	rec := postLogin(t, h, `{"email":"`+email+`","password":"wrongpass"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "INVALID_CREDENTIALS") {
		t.Errorf("body = %s, want INVALID_CREDENTIALS", rec.Body.String())
	}
}

func TestLoginUnknownEmail(t *testing.T) {
	h, _ := newAuth(t)
	rec := postLogin(t, h, `{"email":"nobody@example.com","password":"abc12345"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "INVALID_CREDENTIALS") {
		t.Errorf("body = %s, want INVALID_CREDENTIALS (same as wrong password)", rec.Body.String())
	}
}

func TestLoginInactiveAccount(t *testing.T) {
	h, pool := newAuth(t)
	email := "login-inactive@example.com"
	seedUser(t, pool, email, "abc12345", "inactive")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	rec := postLogin(t, h, `{"email":"`+email+`","password":"abc12345"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ACCOUNT_INACTIVE") {
		t.Errorf("body = %s, want ACCOUNT_INACTIVE", rec.Body.String())
	}
}
