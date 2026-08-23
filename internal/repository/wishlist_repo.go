package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"ecommerce/server/internal/model"
)

var ErrAlreadyInWishlist = errors.New("product already in wishlist")

type WishlistRepo struct {
	pool *pgxpool.Pool
}

func NewWishlistRepo(pool *pgxpool.Pool) *WishlistRepo {
	return &WishlistRepo{pool: pool}
}

// Add inserts a wishlist entry. Returns ErrNotFound when the product does not
// exist or is soft-deleted (not wishlistable, PRD C.5), and
// ErrAlreadyInWishlist on duplicate (unique user+product).
func (r *WishlistRepo) Add(ctx context.Context, userID, productID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO wishlists (user_id, product_id)
		 SELECT $1, $2
		 WHERE EXISTS (SELECT 1 FROM products WHERE id = $2 AND deleted_at IS NULL)`,
		userID, productID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrAlreadyInWishlist
		}
		return err
	}
	// INSERT...SELECT inserts 0 rows when the product is missing.
	var exists bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM products WHERE id = $1 AND deleted_at IS NULL)`, productID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

// List returns the user's wishlist with current product state. Soft-deleted
// products are excluded (PRD C.5).
func (r *WishlistRepo) List(ctx context.Context, userID string) ([]*model.WishlistItem, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT p.id, p.name, p.price::bigint, p.stock, p.is_active, w.created_at
		 FROM wishlists w
		 JOIN products p ON p.id = w.product_id
		 WHERE w.user_id = $1 AND p.deleted_at IS NULL
		 ORDER BY w.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*model.WishlistItem
	for rows.Next() {
		item := &model.WishlistItem{}
		if err := rows.Scan(&item.ProductID, &item.ProductName, &item.Price, &item.Stock, &item.IsActive, &item.AddedAt); err != nil {
			return nil, err
		}
		item.InStock = item.Stock > 0
		items = append(items, item)
	}
	return items, rows.Err()
}

// Remove deletes a wishlist entry. Removing a non-existent entry is a no-op
// (idempotent, PRD C.5).
func (r *WishlistRepo) Remove(ctx context.Context, userID, productID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM wishlists WHERE user_id = $1 AND product_id = $2`, userID, productID)
	return err
}
