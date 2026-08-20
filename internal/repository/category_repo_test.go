package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"ecommerce/server/internal/model"
)

func uniqueSlug(base string) string {
	return fmt.Sprintf("%s-%d", base, time.Now().UnixNano())
}

func TestCategoryRepoCreateFindUpdate(t *testing.T) {
	ctx := context.Background()
	repo := NewCategoryRepo(newTestPool(t))

	slug := uniqueSlug("pakaian")
	c, err := repo.Create(ctx, "Pakaian", slug)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	defer func() {
		repo.pool.Exec(ctx, `DELETE FROM categories WHERE id = $1`, c.ID)
	}()

	if c.ID == "" || c.Slug != slug || !c.IsActive {
		t.Errorf("Create() = %+v, want slug=%s is_active=true", c, slug)
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

	updSlug := uniqueSlug("pakaian-pria")
	upd, err := repo.Update(ctx, c.ID, "Pakaian Pria", updSlug)
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if upd.Name != "Pakaian Pria" || upd.Slug != updSlug {
		t.Errorf("Update() = %+v, want name+slug updated", upd)
	}
}

func TestCategoryRepoSoftDeleteExcludedFromActive(t *testing.T) {
	ctx := context.Background()
	repo := NewCategoryRepo(newTestPool(t))

	c, err := repo.Create(ctx, "Elektronik", uniqueSlug("elektronik"))
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

	slug := uniqueSlug("aksesoris")
	c, err := repo.Create(ctx, "Aksesoris", slug)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		repo.pool.Exec(ctx, `DELETE FROM categories WHERE id = $1`, c.ID)
	}()

	_, err = repo.Create(ctx, "Aksesoris Lain", slug)
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
