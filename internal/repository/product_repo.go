package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

func (r *ProductRepo) SetActive(ctx context.Context, id string, isActive bool) (*model.Product, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE products
		 SET is_active = $2, updated_at = now()
		 WHERE id = $1
		 RETURNING `+productColumns,
		id, isActive)

	p, err := scanProduct(row)
	if err != nil {
		return nil, mapProductError(err)
	}
	return p, nil
}

func (r *ProductRepo) SetStock(ctx context.Context, id string, stock int) (*model.Product, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE products
		 SET stock = $2, updated_at = now()
		 WHERE id = $1
		 RETURNING `+productColumns,
		id, stock)

	p, err := scanProduct(row)
	if err != nil {
		return nil, mapProductError(err)
	}
	return p, nil
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

type ProductFilter struct {
	Search     string
	CategoryID string
	MinPrice   int64
	MaxPrice   int64
	HasMin     bool
	HasMax     bool
	InStock    bool
	Sort       string
	Limit      int
	Offset     int
}

const publicProductColumns = `p.id, p.category_id, p.name, p.description, p.price::bigint, p.stock, p.is_active, p.deleted_at, p.created_at, p.updated_at, c.id, c.name`

// ListPublic returns non-deleted, active products matching the filter, with
// category joined in and the total count for pagination meta. Empty filter
// fields are ignored; sort defaults to newest.
func (r *ProductRepo) ListPublic(ctx context.Context, f ProductFilter) ([]*model.Product, int64, error) {
	where := `p.deleted_at IS NULL AND p.is_active = true`
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if s := strings.TrimSpace(f.Search); s != "" {
		pattern := "%" + s + "%"
		where += fmt.Sprintf(` AND (p.name ILIKE %s OR p.description ILIKE %s)`, arg(pattern), arg(pattern))
	}
	if f.CategoryID != "" {
		where += fmt.Sprintf(` AND p.category_id = %s`, arg(f.CategoryID))
	}
	if f.HasMin {
		where += fmt.Sprintf(` AND p.price >= %s`, arg(f.MinPrice))
	}
	if f.HasMax {
		where += fmt.Sprintf(` AND p.price <= %s`, arg(f.MaxPrice))
	}
	if f.InStock {
		where += ` AND p.stock > 0`
	}

	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM products p WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	order := "p.created_at DESC"
	switch f.Sort {
	case "price_asc":
		order = "p.price ASC, p.created_at DESC"
	case "price_desc":
		order = "p.price DESC, p.created_at DESC"
	case "name_asc":
		order = "p.name ASC, p.created_at DESC"
	}

	query := `SELECT ` + publicProductColumns + `
		FROM products p
		LEFT JOIN categories c ON c.id = p.category_id
		WHERE ` + where + `
		ORDER BY ` + order + `
		LIMIT ` + arg(f.Limit) + ` OFFSET ` + arg(f.Offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var products []*model.Product
	for rows.Next() {
		p, err := scanPublicProduct(rows)
		if err != nil {
			return nil, 0, err
		}
		products = append(products, p)
	}
	return products, total, rows.Err()
}

func scanPublicProduct(row rowScanner) (*model.Product, error) {
	p := &model.Product{}
	var catID, catName *string
	err := row.Scan(&p.ID, &p.CategoryID, &p.Name, &p.Description, &p.Price, &p.Stock, &p.IsActive, &p.DeletedAt, &p.CreatedAt, &p.UpdatedAt, &catID, &catName)
	if err != nil {
		return nil, err
	}
	if catID != nil {
		p.Category = &model.Category{ID: *catID, Name: *catName}
	}
	return p, nil
}

// FindPublicByID returns a non-deleted, active product with its category and
// images. Returns ErrNotFound if the product is missing, inactive, or
// soft-deleted (not visible publicly, PRD C.3).
func (r *ProductRepo) FindPublicByID(ctx context.Context, id string) (*model.Product, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+publicProductColumns+`
		 FROM products p
		 LEFT JOIN categories c ON c.id = p.category_id
		 WHERE p.id = $1 AND p.deleted_at IS NULL AND p.is_active = true`, id)

	p, err := scanPublicProduct(row)
	if err != nil {
		return nil, mapProductError(err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT id, url, is_primary, display_order, created_at
		 FROM product_images
		 WHERE product_id = $1
		 ORDER BY display_order, created_at`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var img model.ProductImage
		if err := rows.Scan(&img.ID, &img.URL, &img.IsPrimary, &img.DisplayOrder, &img.CreatedAt); err != nil {
			return nil, err
		}
		p.Images = append(p.Images, img)
	}
	return p, rows.Err()
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

var ErrImageNotFound = errors.New("product image does not exist")

// CreateImage inserts a product image and returns it.
func (r *ProductRepo) CreateImage(ctx context.Context, productID, url string, displayOrder int) (*model.ProductImage, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO product_images (product_id, url, display_order)
		 VALUES ($1, $2, $3)
		 RETURNING id, url, is_primary, display_order, created_at`,
		productID, url, displayOrder)

	img := &model.ProductImage{}
	if err := row.Scan(&img.ID, &img.URL, &img.IsPrimary, &img.DisplayOrder, &img.CreatedAt); err != nil {
		return nil, mapProductError(err)
	}
	return img, nil
}

// FindImage returns an image belonging to the product (for ownership check
// before delete).
func (r *ProductRepo) FindImage(ctx context.Context, productID, imageID string) (*model.ProductImage, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, url, is_primary, display_order, created_at
		 FROM product_images
		 WHERE id = $1 AND product_id = $2`, imageID, productID)

	img := &model.ProductImage{}
	if err := row.Scan(&img.ID, &img.URL, &img.IsPrimary, &img.DisplayOrder, &img.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrImageNotFound
		}
		return nil, err
	}
	return img, nil
}

// DeleteImage removes the image row from the DB (storage object is removed by
// the service).
func (r *ProductRepo) DeleteImage(ctx context.Context, productID, imageID string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM product_images WHERE id = $1 AND product_id = $2`, imageID, productID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrImageNotFound
	}
	return nil
}
