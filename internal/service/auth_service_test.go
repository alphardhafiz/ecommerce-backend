package service

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	jwtpkg "ecommerce/server/internal/jwt"
	"ecommerce/server/internal/repository"
)

type fakeMailer struct {
	sentTo []string
}

func (f *fakeMailer) SendPasswordReset(to, resetLink string) error {
	f.sentTo = append(f.sentTo, to)
	return nil
}

func newAuthSvc(t *testing.T) (*AuthService, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping auth service DB test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	jwtHelper := jwtpkg.New("test-secret", jwtpkg.DefaultTTL)
	svc := NewAuthService(
		repository.NewUserRepo(pool),
		repository.NewRefreshTokenRepo(pool),
		repository.NewPasswordResetTokenRepo(pool),
		jwtHelper,
		&fakeMailer{},
		"http://localhost:3000",
	)
	return svc, pool
}

func TestAuthServiceRefreshReuse(t *testing.T) {
	svc, pool := newAuthSvc(t)
	ctx := context.Background()
	email := "svc-refresh-reuse@example.com"
	pool.Exec(ctx, `DELETE FROM users WHERE email = $1`, email)
	defer pool.Exec(ctx, `DELETE FROM users WHERE email = $1`, email)

	if _, err := svc.Register(ctx, "Budi", email, "abc12345", "abc12345"); err != nil {
		t.Fatal(err)
	}
	login, err := svc.Login(ctx, email, "abc12345")
	if err != nil {
		t.Fatal(err)
	}

	// First refresh succeeds (rotation).
	refreshed, err := svc.Refresh(ctx, login.RefreshToken)
	if err != nil {
		t.Fatalf("first refresh failed: %v", err)
	}
	if refreshed.RefreshToken == login.RefreshToken {
		t.Error("refresh token should be rotated")
	}

	// Reusing the old (already rotated) token must fail uniformly.
	if _, err := svc.Refresh(ctx, login.RefreshToken); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("reuse error = %v, want ErrInvalidRefreshToken", err)
	}
}

func TestRegisterValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     [4]string // name, email, password, confirm
		wantErr bool
	}{
		{"empty name", [4]string{"", "a@b.com", "abc12345", "abc12345"}, true},
		{"bad email", [4]string{"A", "not-an-email", "abc12345", "abc12345"}, true},
		{"short password", [4]string{"A", "a@b.com", "abc123", "abc123"}, true},
		{"password no digit", [4]string{"A", "a@b.com", "abcdefgh", "abcdefgh"}, true},
		{"password no letter", [4]string{"A", "a@b.com", "12345678", "12345678"}, true},
		{"confirm mismatch", [4]string{"A", "a@b.com", "abc12345", "abc12346"}, true},
		{"valid", [4]string{"Budi", "a@b.com", "abc12345", "abc12345"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verrs := validateRegister(tt.req[0], tt.req[1], tt.req[2], tt.req[3])
			if tt.wantErr {
				if len(verrs) == 0 {
					t.Errorf("validateRegister() = no errors, want validation errors for %v", tt.req)
				}
			} else if len(verrs) > 0 {
				t.Errorf("validateRegister() = %v, want no errors", verrs)
			}
		})
	}
}
