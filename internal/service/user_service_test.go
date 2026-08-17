package service

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"ecommerce/server/internal/repository"
)

func newUserSvc(t *testing.T) (*UserService, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping user service DB test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return NewUserService(repository.NewUserRepo(pool)), pool
}

func TestUserServiceUpdateMeEmptyName(t *testing.T) {
	svc, pool := newUserSvc(t)
	ctx := context.Background()
	email := "svc-user-me-empty@example.com"
	pool.Exec(ctx, `DELETE FROM users WHERE email = $1`, email)
	defer pool.Exec(ctx, `DELETE FROM users WHERE email = $1`, email)

	user, err := svc.users.Create(ctx, "Budi", email, "x")
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.UpdateMe(ctx, user.ID, "  ", nil)
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
	if len(verr.Errors) != 1 || verr.Errors[0].Field != "name" {
		t.Errorf("errors = %+v, want single name error", verr.Errors)
	}
}

func TestUserServiceUpdateStatusInvalid(t *testing.T) {
	svc, pool := newUserSvc(t)
	ctx := context.Background()
	email := "svc-user-status-invalid@example.com"
	pool.Exec(ctx, `DELETE FROM users WHERE email = $1`, email)
	defer pool.Exec(ctx, `DELETE FROM users WHERE email = $1`, email)

	user, err := svc.users.Create(ctx, "Budi", email, "x")
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.UpdateStatus(ctx, user.ID, "deleted")
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
	if len(verr.Errors) != 1 || verr.Errors[0].Field != "status" {
		t.Errorf("errors = %+v, want single status error", verr.Errors)
	}
}

func TestUserServiceUpdateStatusNotFound(t *testing.T) {
	svc, _ := newUserSvc(t)
	_, err := svc.UpdateStatus(context.Background(), "00000000-0000-0000-0000-000000000000", "inactive")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}
