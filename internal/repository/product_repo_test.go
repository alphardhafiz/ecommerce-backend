package repository

import (
	"context"
	"errors"
	"testing"
)

func TestProductRepoCreateFindUpdate(t *testing.T) {
	ctx := context.Background()
	repo := NewProductRepo(newTestPool(t))

	p, err := repo.Create(ctx, "Kaos Polos", nil, 89000, 15, nil)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	defer func() {
		repo.pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, p.ID)
	}()

	if p.ID == "" || p.Price != 89000 || p.Stock != 15 || !p.IsActive {
		t.Errorf("Create() = %+v, want price=89000 stock=15 active", p)
	}
	if p.DeletedAt != nil {
		t.Error("Create() DeletedAt should be nil")
	}

	got, err := repo.FindByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("FindByID() error: %v", err)
	}
	if got.Name != "Kaos Polos" || got.Price != 89000 {
		t.Errorf("FindByID() = %+v, want name+price", got)
	}

	desc := "Katun combed 30s"
	upd, err := repo.Update(ctx, p.ID, "Kaos Polos Premium", &desc, 99000, 10, nil)
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if upd.Name != "Kaos Polos Premium" || upd.Price != 99000 || upd.Stock != 10 || upd.Description == nil || *upd.Description != desc {
		t.Errorf("Update() = %+v, want all fields updated", upd)
	}
}

func TestProductRepoSoftDeleteExcludedFromActive(t *testing.T) {
	ctx := context.Background()
	repo := NewProductRepo(newTestPool(t))

	p, err := repo.Create(ctx, "Sepatu Lari", nil, 250000, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		repo.pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, p.ID)
	}()

	_, _, err = repo.ListActive(ctx, 50, 0)
	if err != nil {
		t.Fatalf("ListActive() error: %v", err)
	}

	if err := repo.SoftDelete(ctx, p.ID); err != nil {
		t.Fatalf("SoftDelete() error: %v", err)
	}

	list, total, err := repo.ListActive(ctx, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list {
		if item.ID == p.ID {
			t.Error("ListActive() still contains soft-deleted product")
		}
	}
	// total is the pre-filter count of active products; find this product row directly.
	var deleted bool
	repo.pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM products WHERE id = $1`, p.ID).Scan(&deleted)
	if !deleted {
		t.Error("SoftDelete() did not set deleted_at")
	}
	_ = total

	if err := repo.SoftDelete(ctx, p.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second SoftDelete() error = %v, want ErrNotFound", err)
	}
}

func TestProductRepoListActivePagination(t *testing.T) {
	ctx := context.Background()
	repo := NewProductRepo(newTestPool(t))

	ids := []string{}
	defer func() {
		repo.pool.Exec(ctx, `DELETE FROM products WHERE id = ANY($1)`, ids)
	}()
	for _, name := range []string{"P1", "P2", "P3", "P4", "P5"} {
		p, err := repo.Create(ctx, name, nil, 10000, 1, nil)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}

	page, total, err := repo.ListActive(ctx, 2, 0)
	if err != nil {
		t.Fatalf("ListActive() error: %v", err)
	}
	if len(page) != 2 {
		t.Errorf("ListActive() page size = %d, want 2", len(page))
	}
	if total < 5 {
		t.Errorf("ListActive() total = %d, want >= 5", total)
	}

	page2, _, err := repo.ListActive(ctx, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 {
		t.Errorf("ListActive() offset page size = %d, want 2", len(page2))
	}
	if page[0].ID == page2[0].ID {
		t.Error("ListActive() pages overlap")
	}
}

func TestProductRepoFindByIDNotFound(t *testing.T) {
	repo := NewProductRepo(newTestPool(t))
	_, err := repo.FindByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("FindByID() error = %v, want ErrNotFound", err)
	}
}
