package repository

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping repository test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestUserRepoCreateFindUpdate(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepo(newTestPool(t))

	u, err := repo.Create(ctx, "Test User", "repo-test@example.com", "hash")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	defer func() {
		repo.pool.Exec(ctx, `DELETE FROM users WHERE email = $1`, "repo-test@example.com")
	}()

	if u.ID == "" {
		t.Error("Create() returned empty id")
	}
	if u.Role != "user" || u.Status != "active" {
		t.Errorf("Create() role=%q status=%q, want default user/active", u.Role, u.Status)
	}
	if u.PasswordHash != "hash" {
		t.Errorf("PasswordHash = %q, want hash", u.PasswordHash)
	}

	got, err := repo.FindByEmail(ctx, "repo-test@example.com")
	if err != nil {
		t.Fatalf("FindByEmail() error: %v", err)
	}
	if got.ID != u.ID {
		t.Errorf("FindByEmail() id = %q, want %q", got.ID, u.ID)
	}

	phone := "08123456789"
	updated, err := repo.Update(ctx, u.ID, "Test User Baru", &phone)
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if updated.Name != "Test User Baru" || updated.Phone == nil || *updated.Phone != phone {
		t.Errorf("Update() = %+v, want name+phone updated", updated)
	}
	if updated.Role != "user" || updated.Status != "active" {
		t.Errorf("Update() changed role/status: role=%q status=%q", updated.Role, updated.Status)
	}
}

func TestUserRepoFindByEmailNotFound(t *testing.T) {
	repo := NewUserRepo(newTestPool(t))
	_, err := repo.FindByEmail(context.Background(), "tidak-ada@example.com")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("FindByEmail() error = %v, want ErrNotFound", err)
	}
}

func TestUserRepoCreateDuplicateEmail(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepo(newTestPool(t))

	repo.Create(ctx, "Dup User", "dup@example.com", "hash")
	defer func() {
		repo.pool.Exec(ctx, `DELETE FROM users WHERE email = $1`, "dup@example.com")
	}()

	_, err := repo.Create(ctx, "Dup User 2", "dup@example.com", "hash2")
	if !errors.Is(err, ErrEmailTaken) {
		t.Errorf("Create() duplicate error = %v, want ErrEmailTaken", err)
	}
}
