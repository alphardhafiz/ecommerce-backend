package repository

import (
	"context"
	"errors"
	"testing"

	"ecommerce/server/internal/model"
)

func TestCategoryRepoCreateFindUpdate(t *testing.T) {
	ctx := context.Background()
	repo := NewCategoryRepo(newTestPool(t))

	c, err := repo.Create(ctx, "Pakaian", "pakaian")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	defer func() {
		repo.pool.Exec(ctx, `DELETE FROM categories WHERE id = $1`, c.ID)
	}()

	if c.ID == "" || c.Slug != "pakaian" || !c.IsActive {
		t.Errorf("Create() = %+v, want slug=pakaian is_active=true", c)
	}
	if c.DeletedAt != nil {
		t.Error("Create() DeletedAt should be nil")
	}

	got, err := repo.FindByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("FindByID() error: %v", err)
	}
	if got.Name != "Pakaian" {
		t.Errorf("FindByID() name = %q, want Pakaian", got.Name)
	}

	upd, err := repo.Update(ctx, c.ID, "Pakaian Pria", "pakaian-pria")
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if upd.Name != "Pakaian Pria" || upd.Slug != "pakaian-pria" {
		t.Errorf("Update() = %+v, want name+slug updated", upd)
	}
}

func TestCategoryRepoSoftDeleteExcludedFromActive(t *testing.T) {
	ctx := context.Background()
	repo := NewCategoryRepo(newTestPool(t))

	c, err := repo.Create(ctx, "Elektronik", "elektronik")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		repo.pool.Exec(ctx, `DELETE FROM categories WHERE id = $1`, c.ID)
	}()

	list, err := repo.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive() error: %v", err)
	}
	if !containsCategory(list, c.ID) {
		t.Fatalf("ListActive() missing fresh category %s", c.ID)
	}

	if err := repo.SoftDelete(ctx, c.ID); err != nil {
		t.Fatalf("SoftDelete() error: %v", err)
	}

	list, err = repo.ListActive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if containsCategory(list, c.ID) {
		t.Error("ListActive() still contains soft-deleted category")
	}

	// Soft-deleting twice → not found (already deleted).
	if err := repo.SoftDelete(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second SoftDelete() error = %v, want ErrNotFound", err)
	}
}

func TestCategoryRepoSlugTaken(t *testing.T) {
	ctx := context.Background()
	repo := NewCategoryRepo(newTestPool(t))

	c, err := repo.Create(ctx, "Aksesoris", "aksesoris")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		repo.pool.Exec(ctx, `DELETE FROM categories WHERE id = $1`, c.ID)
	}()

	_, err = repo.Create(ctx, "Aksesoris Lain", "aksesoris")
	if !errors.Is(err, ErrSlugTaken) {
		t.Errorf("duplicate slug error = %v, want ErrSlugTaken", err)
	}
}

func TestCategoryRepoFindByIDNotFound(t *testing.T) {
	repo := NewCategoryRepo(newTestPool(t))
	_, err := repo.FindByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("FindByID() error = %v, want ErrNotFound", err)
	}
}

func containsCategory(list []*model.Category, id string) bool {
	for _, c := range list {
		if c.ID == id {
			return true
		}
	}
	return false
}
