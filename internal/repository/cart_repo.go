package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ecommerce/server/internal/model"
)

type CartRepo struct {
	pool *pgxpool.Pool
}

func NewCartRepo(pool *pgxpool.Pool) *CartRepo {
	return &CartRepo{pool: pool}
}

// GetOrCreate lazily creates the user's cart (1 user = 1 cart, UNIQUE
// user_id) and returns it — racing calls converge on a single row.
func (r *CartRepo) GetOrCreate(ctx context.Context, userID string) (*model.Cart, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO carts (user_id)
		 VALUES ($1)
		 ON CONFLICT (user_id) DO UPDATE SET updated_at = carts.updated_at
		 RETURNING id, user_id, created_at, updated_at`, userID)

	c := &model.Cart{}
	if err := row.Scan(&c.ID, &c.UserID, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return c, nil
}

// AddItem adds a product to the cart; if the product is already present the
// quantity is merged (PRD C.6), not duplicated. Missing product surfaces as
// an FK violation (23503).
func (r *CartRepo) AddItem(ctx context.Context, cartID, productID string, quantity int) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO cart_items (cart_id, product_id, quantity)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (cart_id, product_id)
		 DO UPDATE SET quantity = cart_items.quantity + $3, updated_at = now()`,
		cartID, productID, quantity)
	return err
}

// ListItems returns the cart lines joined with current product state. Items
// whose product is inactive or soft-deleted are flagged is_available=false;
// they are excluded from totals by callers (PRD C.6).
func (r *CartRepo) ListItems(ctx context.Context, cartID string) ([]*model.CartItem, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT ci.id, p.id, p.name, p.price::bigint, p.stock, ci.quantity,
		        p.is_active, p.deleted_at IS NOT NULL,
		        (SELECT pi.url FROM product_images pi WHERE pi.product_id = p.id AND pi.is_primary = true LIMIT 1)
		 FROM cart_items ci
		 JOIN products p ON p.id = ci.product_id
		 WHERE ci.cart_id = $1
		 ORDER BY ci.created_at`, cartID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*model.CartItem
	for rows.Next() {
		item := &model.CartItem{}
		var isActive bool
		var isDeleted bool
		if err := rows.Scan(&item.ID, &item.ProductID, &item.Name, &item.Price, &item.Stock, &item.Quantity, &isActive, &isDeleted, &item.PrimaryImage); err != nil {
			return nil, err
		}
		item.Subtotal = item.Price * int64(item.Quantity)
		item.IsAvailable = isActive && !isDeleted
		items = append(items, item)
	}
	return items, rows.Err()
}

// FindItem returns a cart line by its id, joined with the owning cart's
// user_id (for ownership checks) and the product's current stock. Returns
// ErrNotFound when the line does not exist.
func (r *CartRepo) FindItem(ctx context.Context, itemID string) (*model.CartItem, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT ci.id, c.user_id, ci.cart_id, p.id, p.name, p.price::bigint,
		        p.stock, ci.quantity
		 FROM cart_items ci
		 JOIN carts c ON c.id = ci.cart_id
		 JOIN products p ON p.id = ci.product_id
		 WHERE ci.id = $1`, itemID)

	item := &model.CartItem{}
	if err := row.Scan(&item.ID, &item.UserID, &item.CartID, &item.ProductID, &item.Name, &item.Price, &item.Stock, &item.Quantity); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return item, nil
}

// UpdateQuantity sets a cart line's quantity.
func (r *CartRepo) UpdateQuantity(ctx context.Context, itemID string, quantity int) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE cart_items SET quantity = $2, updated_at = now() WHERE id = $1`, itemID, quantity)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RemoveItem deletes a cart line by its id.
func (r *CartRepo) RemoveItem(ctx context.Context, itemID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM cart_items WHERE id = $1`, itemID)
	return err
}

// Clear removes all lines from the cart.
func (r *CartRepo) Clear(ctx context.Context, cartID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM cart_items WHERE cart_id = $1`, cartID)
	return err
}
