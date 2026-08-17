package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jwtpkg "ecommerce/server/internal/jwt"
)

func TestRequireAuthValidToken(t *testing.T) {
	jwth := jwtpkg.New("test-secret", jwtpkg.DefaultTTL)
	mw := RequireAuth(jwth)
	token, err := jwth.Generate("user-1", "user")
	if err != nil {
		t.Fatal(err)
	}

	var gotUserID, gotRole string
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFrom(r.Context())
		if !ok {
			t.Error("claims not in context")
		}
		gotUserID, gotRole = claims.UserID, claims.Role
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if gotUserID != "user-1" || gotRole != "user" {
		t.Errorf("claims = user_id=%q role=%q, want user-1/user", gotUserID, gotRole)
	}
}

func TestRequireAuthExpiredToken(t *testing.T) {
	jwth := jwtpkg.New("test-secret", time.Millisecond)
	mw := RequireAuth(jwth)
	token, err := jwth.Generate("user-1", "user")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "TOKEN_EXPIRED") {
		t.Errorf("body = %s, want TOKEN_EXPIRED", rec.Body.String())
	}
}

func TestRequireAuthMissingHeader(t *testing.T) {
	jwth := jwtpkg.New("test-secret", jwtpkg.DefaultTTL)
	h := RequireAuth(jwth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "UNAUTHORIZED") {
		t.Errorf("body = %s, want UNAUTHORIZED", rec.Body.String())
	}
}

func TestRequireAuthInvalidToken(t *testing.T) {
	jwth := jwtpkg.New("test-secret", jwtpkg.DefaultTTL)
	h := RequireAuth(jwth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "TOKEN_EXPIRED") {
		t.Errorf("body = %s, want generic UNAUTHORIZED not TOKEN_EXPIRED", rec.Body.String())
	}
}

func TestRequireRole(t *testing.T) {
	jwth := jwtpkg.New("test-secret", jwtpkg.DefaultTTL)
	stack := RequireAuth(jwth)(RequireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})))

	tests := []struct {
		name    string
		role    string
		want    int
		wantErr string
	}{
		{"admin allowed", "admin", http.StatusOK, ""},
		{"user forbidden", "user", http.StatusForbidden, "FORBIDDEN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := jwth.Generate("user-1", tt.role)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, "/admin", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			stack.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
			if tt.wantErr != "" && !strings.Contains(rec.Body.String(), tt.wantErr) {
				t.Errorf("body = %s, want %s", rec.Body.String(), tt.wantErr)
			}
		})
	}
}

func TestRequireRoleWithoutAuth(t *testing.T) {
	h := RequireRole("admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (no claims in context)", rec.Code)
	}
}

func TestClaimsFromEmpty(t *testing.T) {
	if _, ok := ClaimsFrom(context.Background()); ok {
		t.Error("ClaimsFrom on empty context should return ok=false")
	}
}
