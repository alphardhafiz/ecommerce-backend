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
	return NewAuth(service.NewAuthService(repository.NewUserRepo(pool))), pool
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
