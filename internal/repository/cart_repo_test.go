package repository

import (
	"context"
	"testing"

	"ecommerce/server/internal/model"
)

func TestCartRepoLazyGetOrCreate(t *testing.T) {
	ctx := context.Background()
	repo := NewCartRepo(newTestPool(t))

	email := "cart-lazy@example.com"
	seed := NewUserRepo(newTestPool(t))
	u, err := seed.Create(ctx, "Cart User", email, "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		repo.pool.Exec(ctx, `DELETE FROM carts WHERE user_id = $1`, u.ID)
		repo.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, u.ID)
	}()

	// Lazy: cart does not exist yet.
	var n int
	repo.pool.QueryRow(ctx, `SELECT count(*) FROM carts WHERE user_id = $1`, u.ID).Scan(&n)
	if n != 0 {
		t.Fatalf("cart must not exist before first access, found %d", n)
	}

	c1, err := repo.GetOrCreate(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetOrCreate() error: %v", err)
	}
	if c1.ID == "" || c1.UserID != u.ID {
		t.Errorf("cart = %+v, want id+user", c1)
	}

	// Second call returns the same cart (1 user = 1 cart).
	c2, err := repo.GetOrCreate(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c2.ID != c1.ID {
		t.Errorf("GetOrCreate() = %s, want same cart %s", c2.ID, c1.ID)
	}
}

func TestCartRepoAddItemMergesQuantity(t *testing.T) {
	ctx := context.Background()
	repo := NewCartRepo(newTestPool(t))

	seed := NewUserRepo(newTestPool(t))
	u, err := seed.Create(ctx, "Cart Merge", "cart-merge@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		repo.pool.Exec(ctx, `DELETE FROM carts WHERE user_id = $1`, u.ID)
		repo.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, u.ID)
	}()

	cart, err := repo.GetOrCreate(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}

	p := NewProductRepo(newTestPool(t))
	prod, err := p.Create(ctx, "Produk Cart", nil, 10000, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		repo.pool.Exec(ctx, `DELETE FROM cart_items WHERE cart_id = $1`, cart.ID)
		repo.pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, prod.ID)
	}()

	if err := repo.AddItem(ctx, cart.ID, prod.ID, 2); err != nil {
		t.Fatalf("AddItem() error: %v", err)
	}
	if err := repo.AddItem(ctx, cart.ID, prod.ID, 3); err != nil {
		t.Fatalf("AddItem() merge error: %v", err)
	}

	var qty int
	repo.pool.QueryRow(ctx, `SELECT quantity FROM cart_items WHERE cart_id = $1 AND product_id = $2`, cart.ID, prod.ID).Scan(&qty)
	if qty != 5 {
		t.Errorf("quantity = %d, want 5 (merged 2+3, not duplicated)", qty)
	}

	items, err := repo.ListItems(ctx, cart.ID)
	if err != nil {
		t.Fatalf("ListItems() error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ListItems() = %d items, want 1 (merged)", len(items))
	}
	item := items[0]
	if item.Name != "Produk Cart" || item.Price != 10000 || item.Quantity != 5 || item.Subtotal != 50000 {
		t.Errorf("item = %+v, want name/price/qty/subtotal", item)
	}
	if !item.IsAvailable {
		t.Error("active product must be is_available=true")
	}
}

func TestCartRepoListItemsFlagsUnavailable(t *testing.T) {
	ctx := context.Background()
	repo := NewCartRepo(newTestPool(t))

	seed := NewUserRepo(newTestPool(t))
	u, err := seed.Create(ctx, "Cart Flag", "cart-flag@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		repo.pool.Exec(ctx, `DELETE FROM carts WHERE user_id = $1`, u.ID)
		repo.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, u.ID)
	}()

	cart, err := repo.GetOrCreate(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}

	p := NewProductRepo(newTestPool(t))
	good, err := p.Create(ctx, "Aktif", nil, 10000, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	inactive, err := p.Create(ctx, "Nonaktif", nil, 10000, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := p.Create(ctx, "Dihapus", nil, 10000, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		repo.pool.Exec(ctx, `DELETE FROM cart_items WHERE cart_id = $1`, cart.ID)
		repo.pool.Exec(ctx, `DELETE FROM products WHERE id = ANY($1)`, []string{good.ID, inactive.ID, deleted.ID})
	}()

	if _, err := p.SetActive(ctx, inactive.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := p.SoftDelete(ctx, deleted.ID); err != nil {
		t.Fatal(err)
	}

	for _, pid := range []string{good.ID, inactive.ID, deleted.ID} {
		if err := repo.AddItem(ctx, cart.ID, pid, 1); err != nil {
			t.Fatal(err)
		}
	}

	items, err := repo.ListItems(ctx, cart.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("ListItems() = %d, want 3 (soft-deleted still listed with flag)", len(items))
	}

	byProduct := map[string]*model.CartItem{}
	for _, item := range items {
		byProduct[item.ProductID] = item
	}
	if !byProduct[good.ID].IsAvailable {
		t.Error("active product must be available")
	}
	if byProduct[inactive.ID].IsAvailable {
		t.Error("inactive product must be is_available=false")
	}
	if byProduct[deleted.ID].IsAvailable {
		t.Error("soft-deleted product must be is_available=false")
	}
}
