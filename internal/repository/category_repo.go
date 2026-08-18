package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"ecommerce/server/internal/model"
)

var ErrSlugTaken = errors.New("category slug already exists")

const categoryColumns = "id, name, slug, is_active, deleted_at, created_at, updated_at"

type CategoryRepo struct {
	pool *pgxpool.Pool
}

func NewCategoryRepo(pool *pgxpool.Pool) *CategoryRepo {
	return &CategoryRepo{pool: pool}
}

func (r *CategoryRepo) Create(ctx context.Context, name, slug string) (*model.Category, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO categories (name, slug) VALUES ($1, $2) RETURNING `+categoryColumns,
		name, slug)

	c, err := scanCategory(row)
	if err != nil {
		return nil, mapCategoryError(err)
	}
	return c, nil
}

func (r *CategoryRepo) FindByID(ctx context.Context, id string) (*model.Category, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+categoryColumns+` FROM categories WHERE id = $1`, id)

	c, err := scanCategory(row)
	if err != nil {
		return nil, mapCategoryError(err)
	}
	return c, nil
}

func (r *CategoryRepo) Update(ctx context.Context, id, name, slug string) (*model.Category, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE categories
		 SET name = $2, slug = $3, updated_at = now()
		 WHERE id = $1
		 RETURNING `+categoryColumns,
		id, name, slug)

	c, err := scanCategory(row)
	if err != nil {
		return nil, mapCategoryError(err)
	}
	return c, nil
}

// SoftDelete marks the category as deleted. Categories are never hard-deleted
// (PRD D.2). Products referencing it stay intact; the category just stops
// appearing in active listings.
func (r *CategoryRepo) SoftDelete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE categories SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL`,
		id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListActive returns non-deleted, active categories ordered by name.
func (r *CategoryRepo) ListActive(ctx context.Context) ([]*model.Category, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+categoryColumns+` FROM categories
		 WHERE deleted_at IS NULL AND is_active = true
		 ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cats []*model.Category
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		cats = append(cats, c)
	}
	return cats, rows.Err()
}

func scanCategory(row rowScanner) (*model.Category, error) {
	c := &model.Category{}
	err := row.Scan(&c.ID, &c.Name, &c.Slug, &c.IsActive, &c.DeletedAt, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func mapCategoryError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrSlugTaken
	}
	return err
}
