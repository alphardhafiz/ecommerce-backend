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

func TestProductRepoAutoPrimaryImage(t *testing.T) {
	ctx := context.Background()
	repo := NewProductRepo(newTestPool(t))

	p, err := repo.Create(ctx, "Produk Primary", nil, 10000, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, p.ID)
	defer repo.pool.Exec(ctx, `DELETE FROM product_images WHERE product_id = $1`, p.ID)

	first, err := repo.CreateImage(ctx, p.ID, "https://img.example.com/1.png")
	if err != nil {
		t.Fatal(err)
	}
	if !first.IsPrimary {
		t.Error("first image must be is_primary=true")
	}
	if first.DisplayOrder != 0 {
		t.Errorf("first display_order = %d, want 0", first.DisplayOrder)
	}

	second, err := repo.CreateImage(ctx, p.ID, "https://img.example.com/2.png")
	if err != nil {
		t.Fatal(err)
	}
	if second.IsPrimary {
		t.Error("second image must be is_primary=false")
	}
	if second.DisplayOrder != 1 {
		t.Errorf("second display_order = %d, want 1", second.DisplayOrder)
	}
}

func TestProductRepoDeletePrimaryPromotesNext(t *testing.T) {
	ctx := context.Background()
	repo := NewProductRepo(newTestPool(t))

	p, err := repo.Create(ctx, "Produk Promote", nil, 10000, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, p.ID)
	defer repo.pool.Exec(ctx, `DELETE FROM product_images WHERE product_id = $1`, p.ID)

	first, _ := repo.CreateImage(ctx, p.ID, "https://img.example.com/1.png")
	second, _ := repo.CreateImage(ctx, p.ID, "https://img.example.com/2.png")

	if err := repo.DeleteImage(ctx, p.ID, first.ID); err != nil {
		t.Fatalf("DeleteImage() error: %v", err)
	}

	got, err := repo.FindImage(ctx, p.ID, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsPrimary {
		t.Error("after deleting primary, next image must be promoted to primary")
	}
}

func TestProductRepoDeleteNonPrimaryKeepsPrimary(t *testing.T) {
	ctx := context.Background()
	repo := NewProductRepo(newTestPool(t))

	p, err := repo.Create(ctx, "Produk Keep", nil, 10000, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, p.ID)
	defer repo.pool.Exec(ctx, `DELETE FROM product_images WHERE product_id = $1`, p.ID)

	first, _ := repo.CreateImage(ctx, p.ID, "https://img.example.com/1.png")
	second, _ := repo.CreateImage(ctx, p.ID, "https://img.example.com/2.png")

	if err := repo.DeleteImage(ctx, p.ID, second.ID); err != nil {
		t.Fatalf("DeleteImage() error: %v", err)
	}

	got, err := repo.FindImage(ctx, p.ID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsPrimary {
		t.Error("deleting a non-primary image must not change the primary")
	}
}

func TestProductRepoDeleteImageNotFound(t *testing.T) {
	repo := NewProductRepo(newTestPool(t))
	err := repo.DeleteImage(context.Background(), "00000000-0000-0000-0000-000000000000", "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrImageNotFound) {
		t.Errorf("DeleteImage() error = %v, want ErrImageNotFound", err)
	}
}
