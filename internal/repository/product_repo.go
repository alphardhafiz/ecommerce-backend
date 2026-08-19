package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"ecommerce/server/internal/model"
)

var ErrCategoryNotFound = errors.New("category does not exist")

// productColumns casts price (NUMERIC) to bigint so it scans into int64
// (whole rupiah) — money is never a float in Go (PRD D.2).
const productColumns = "id, category_id, name, description, price::bigint, stock, is_active, deleted_at, created_at, updated_at"

type ProductRepo struct {
	pool *pgxpool.Pool
}

func NewProductRepo(pool *pgxpool.Pool) *ProductRepo {
	return &ProductRepo{pool: pool}
}

func (r *ProductRepo) Create(ctx context.Context, name string, description *string, price, stock int64, categoryID *string) (*model.Product, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO products (name, description, price, stock, category_id)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+productColumns,
		name, description, price, stock, categoryID)

	p, err := scanProduct(row)
	if err != nil {
		return nil, mapProductError(err)
	}
	return p, nil
}

func (r *ProductRepo) FindByID(ctx context.Context, id string) (*model.Product, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+productColumns+` FROM products WHERE id = $1`, id)

	p, err := scanProduct(row)
	if err != nil {
		return nil, mapProductError(err)
	}
	return p, nil
}

func (r *ProductRepo) Update(ctx context.Context, id, name string, description *string, price, stock int64, categoryID *string) (*model.Product, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE products
		 SET name = $2, description = $3, price = $4, stock = $5, category_id = $6, updated_at = now()
		 WHERE id = $1
		 RETURNING `+productColumns,
		id, name, description, price, stock, categoryID)

	p, err := scanProduct(row)
	if err != nil {
		return nil, mapProductError(err)
	}
	return p, nil
}

// SoftDelete marks the product as deleted. Products are never hard-deleted:
// order_items/cart_items/wishlists reference product_id (PRD D.2).
func (r *ProductRepo) SoftDelete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE products SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`,
		id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListActive returns non-deleted, active products ordered newest first, with
// the total count matching the same filter (for pagination meta).
func (r *ProductRepo) ListActive(ctx context.Context, limit, offset int) ([]*model.Product, int64, error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM products WHERE deleted_at IS NULL AND is_active = true`).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.pool.Query(ctx,
		`SELECT `+productColumns+` FROM products
		 WHERE deleted_at IS NULL AND is_active = true
		 ORDER BY created_at DESC
		 LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var products []*model.Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, 0, err
		}
		products = append(products, p)
	}
	return products, total, rows.Err()
}

func scanProduct(row rowScanner) (*model.Product, error) {
	p := &model.Product{}
	err := row.Scan(&p.ID, &p.CategoryID, &p.Name, &p.Description, &p.Price, &p.Stock, &p.IsActive, &p.DeletedAt, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func mapProductError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return ErrCategoryNotFound
	}
	return err
}
