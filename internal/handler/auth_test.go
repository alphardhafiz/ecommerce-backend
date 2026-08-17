package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

	var csrfCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "csrf_token" {
			csrfCookie = c
		}
	}
	if csrfCookie == nil {
		t.Fatal("csrf_token cookie not set")
	}
	if csrfCookie.HttpOnly || csrfCookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("csrf cookie: HttpOnly=%v SameSite=%v, want JS-readable + Strict", csrfCookie.HttpOnly, csrfCookie.SameSite)
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

func cookieByName(t *testing.T, rec *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("cookie %q not set in response", name)
	return nil
}

func postRefresh(t *testing.T, h *Auth, refreshToken, csrfToken string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	if refreshToken != "" {
		req.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})
	}
	if csrfToken != "" {
		req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrfToken})
		req.Header.Set("X-CSRF-Token", csrfToken)
	}
	rec := httptest.NewRecorder()
	h.Refresh(rec, req)
	return rec
}

func hashToken(t *testing.T, raw string) string {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("bad base64 token: %v", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestRefreshSuccess(t *testing.T) {
	h, pool := newAuth(t)
	email := "refresh-success@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	loginRec := postLogin(t, h, `{"email":"`+email+`","password":"abc12345"}`)
	oldRefresh := cookieByName(t, loginRec, "refresh_token").Value
	csrf := cookieByName(t, loginRec, "csrf_token").Value

	rec := postRefresh(t, h, oldRefresh, csrf)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int    `json:"expires_in"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || body.Data.AccessToken == "" || body.Data.ExpiresIn != 900 {
		t.Errorf("body = %+v, want success + access_token + expires_in=900", body)
	}

	newRefresh := cookieByName(t, rec, "refresh_token").Value
	if newRefresh == oldRefresh {
		t.Error("refresh token was not rotated (old == new)")
	}

	// Access token must be a valid JWT with correct claims.
	claims, err := jwtpkg.New("test-secret", jwtpkg.DefaultTTL).Validate(body.Data.AccessToken)
	if err != nil {
		t.Fatalf("access token invalid: %v", err)
	}
	var uid string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&uid)
	if claims.UserID != uid || claims.Role != "user" {
		t.Errorf("claims = %+v, want user_id=%s role=user", claims, uid)
	}

	// Old token must be revoked, new token active.
	var oldRevoked bool
	pool.QueryRow(context.Background(),
		`SELECT revoked_at IS NOT NULL FROM refresh_tokens WHERE token_hash = $1`,
		hashToken(t, oldRefresh)).Scan(&oldRevoked)
	if !oldRevoked {
		t.Error("old refresh token should be revoked after rotation")
	}
	var newActive int
	pool.QueryRow(context.Background(),
		`SELECT count(*) FROM refresh_tokens rt JOIN users u ON u.id = rt.user_id WHERE u.email = $1 AND rt.token_hash = $2 AND rt.revoked_at IS NULL`,
		email, hashToken(t, newRefresh)).Scan(&newActive)
	if newActive != 1 {
		t.Errorf("new refresh token not active in DB, count = %d", newActive)
	}
}

func TestRefreshReuseRevokesAllSessions(t *testing.T) {
	h, pool := newAuth(t)
	email := "refresh-reuse@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	loginRec := postLogin(t, h, `{"email":"`+email+`","password":"abc12345"}`)
	oldRefresh := cookieByName(t, loginRec, "refresh_token").Value
	csrf := cookieByName(t, loginRec, "csrf_token").Value

	// First refresh rotates the token. Second refresh with the SAME old token
	// = reuse -> 401 AND all sessions of the user get revoked.
	postRefresh(t, h, oldRefresh, csrf)
	rec := postRefresh(t, h, oldRefresh, csrf)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reuse status = %d, want 401, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "INVALID_REFRESH_TOKEN") {
		t.Errorf("body = %s, want INVALID_REFRESH_TOKEN", rec.Body.String())
	}

	var active int
	pool.QueryRow(context.Background(),
		`SELECT count(*) FROM refresh_tokens rt JOIN users u ON u.id = rt.user_id WHERE u.email = $1 AND rt.revoked_at IS NULL`,
		email).Scan(&active)
	if active != 0 {
		t.Errorf("all sessions should be revoked after reuse, active = %d", active)
	}
}

func TestRefreshExpiredToken(t *testing.T) {
	h, pool := newAuth(t)
	email := "refresh-expired@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	loginRec := postLogin(t, h, `{"email":"`+email+`","password":"abc12345"}`)
	oldRefresh := cookieByName(t, loginRec, "refresh_token").Value
	csrf := cookieByName(t, loginRec, "csrf_token").Value

	pool.Exec(context.Background(),
		`UPDATE refresh_tokens SET expires_at = now() - interval '1 hour' WHERE token_hash = $1`,
		hashToken(t, oldRefresh))

	rec := postRefresh(t, h, oldRefresh, csrf)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "INVALID_REFRESH_TOKEN") {
		t.Errorf("body = %s, want INVALID_REFRESH_TOKEN", rec.Body.String())
	}
}

func TestRefreshUnknownToken(t *testing.T) {
	h, _ := newAuth(t)
	rec := postRefresh(t, h, "totally-bogus-token", "whatever")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "INVALID_REFRESH_TOKEN") {
		t.Errorf("body = %s, want INVALID_REFRESH_TOKEN", rec.Body.String())
	}
}

func TestRefreshMissingCookie(t *testing.T) {
	h, _ := newAuth(t)
	rec := postRefresh(t, h, "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "INVALID_REFRESH_TOKEN") {
		t.Errorf("body = %s, want INVALID_REFRESH_TOKEN", rec.Body.String())
	}
}

func TestRefreshCSRFMismatch(t *testing.T) {
	h, pool := newAuth(t)
	email := "refresh-csrf@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	loginRec := postLogin(t, h, `{"email":"`+email+`","password":"abc12345"}`)
	oldRefresh := cookieByName(t, loginRec, "refresh_token").Value
	csrf := cookieByName(t, loginRec, "csrf_token").Value

	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: oldRefresh})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	req.Header.Set("X-CSRF-Token", "attacker-chosen-value")
	rec := httptest.NewRecorder()
	h.Refresh(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "CSRF_INVALID") {
		t.Errorf("body = %s, want CSRF_INVALID", rec.Body.String())
	}
	// Token must NOT be rotated (no effect).
	var revoked bool
	pool.QueryRow(context.Background(),
		`SELECT revoked_at IS NOT NULL FROM refresh_tokens WHERE token_hash = $1`,
		hashToken(t, oldRefresh)).Scan(&revoked)
	if revoked {
		t.Error("refresh token should not be revoked on CSRF failure")
	}
}

func TestRefreshCSRFMissingHeader(t *testing.T) {
	h, pool := newAuth(t)
	email := "refresh-csrf-missing@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	loginRec := postLogin(t, h, `{"email":"`+email+`","password":"abc12345"}`)
	oldRefresh := cookieByName(t, loginRec, "refresh_token").Value
	csrf := cookieByName(t, loginRec, "csrf_token").Value

	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: oldRefresh})
	req.AddCookie(&http.Cookie{Name: "csrf_token", Value: csrf})
	rec := httptest.NewRecorder()
	h.Refresh(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (missing X-CSRF-Token header)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "CSRF_INVALID") {
		t.Errorf("body = %s, want CSRF_INVALID", rec.Body.String())
	}
}

func TestRefreshInactiveAccount(t *testing.T) {
	h, pool := newAuth(t)
	email := "refresh-inactive@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	loginRec := postLogin(t, h, `{"email":"`+email+`","password":"abc12345"}`)
	oldRefresh := cookieByName(t, loginRec, "refresh_token").Value
	csrf := cookieByName(t, loginRec, "csrf_token").Value

	pool.Exec(context.Background(), `UPDATE users SET status = 'inactive' WHERE email = $1`, email)

	rec := postRefresh(t, h, oldRefresh, csrf)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ACCOUNT_INACTIVE") {
		t.Errorf("body = %s, want ACCOUNT_INACTIVE", rec.Body.String())
	}
}

func TestRefreshReuseRevokesAllDevices(t *testing.T) {
	h, pool := newAuth(t)
	email := "refresh-reuse-multi@example.com"
	seedUser(t, pool, email, "abc12345", "active")
	defer pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)

	// Two independent sessions (two devices).
	dev1 := postLogin(t, h, `{"email":"`+email+`","password":"abc12345"}`)
	dev1Refresh := cookieByName(t, dev1, "refresh_token").Value
	csrf := cookieByName(t, dev1, "csrf_token").Value
	dev2 := postLogin(t, h, `{"email":"`+email+`","password":"abc12345"}`)
	dev2Refresh := cookieByName(t, dev2, "refresh_token").Value

	// Replay dev1's token a second time -> reuse detection, ALL sessions die.
	postRefresh(t, h, dev1Refresh, csrf)
	rec := postRefresh(t, h, dev1Refresh, csrf)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("reuse status = %d, want 401, body: %s", rec.Code, rec.Body.String())
	}

	var dev1Revoked, dev2Revoked bool
	pool.QueryRow(context.Background(),
		`SELECT revoked_at IS NOT NULL FROM refresh_tokens WHERE token_hash = $1`,
		hashToken(t, dev1Refresh)).Scan(&dev1Revoked)
	pool.QueryRow(context.Background(),
		`SELECT revoked_at IS NOT NULL FROM refresh_tokens WHERE token_hash = $1`,
		hashToken(t, dev2Refresh)).Scan(&dev2Revoked)
	if !dev1Revoked || !dev2Revoked {
		t.Errorf("reuse must revoke ALL devices: dev1 revoked=%v dev2 revoked=%v", dev1Revoked, dev2Revoked)
	}
}
