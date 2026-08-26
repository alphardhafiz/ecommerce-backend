package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ecommerce/server/internal/model"
)

type AddressRepo struct {
	pool *pgxpool.Pool
}

func NewAddressRepo(pool *pgxpool.Pool) *AddressRepo {
	return &AddressRepo{pool: pool}
}

const addressCols = `id, user_id, label, recipient_name, phone, full_address, city, province, postal_code, is_default`

func scanAddress(row pgx.Row) (*model.Address, error) {
	a := &model.Address{}
	if err := row.Scan(&a.ID, &a.UserID, &a.Label, &a.RecipientName, &a.Phone, &a.FullAddress, &a.City, &a.Province, &a.PostalCode, &a.IsDefault); err != nil {
		return nil, err
	}
	return a, nil
}

// List returns the user's addresses, default first.
func (r *AddressRepo) List(ctx context.Context, userID string) ([]*model.Address, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+addressCols+` FROM addresses
		 WHERE user_id = $1
		 ORDER BY is_default DESC, created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*model.Address
	for rows.Next() {
		a, err := scanAddress(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// FindByID returns an address by id (with user_id for ownership checks).
// Returns ErrNotFound when it does not exist.
func (r *AddressRepo) FindByID(ctx context.Context, id string) (*model.Address, error) {
	a, err := scanAddress(r.pool.QueryRow(ctx,
		`SELECT `+addressCols+` FROM addresses WHERE id = $1`, id))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return a, nil
}

// Create inserts an address. When is_default=true the user's other
// addresses are unset in the same transaction (PRD C.7, S.13).
func (r *AddressRepo) Create(ctx context.Context, userID string, a *model.Address) (*model.Address, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if a.IsDefault {
		if _, err := tx.Exec(ctx,
			`UPDATE addresses SET is_default = false, updated_at = now() WHERE user_id = $1`, userID); err != nil {
			return nil, err
		}
	}

	created, err := scanAddress(tx.QueryRow(ctx,
		`INSERT INTO addresses (user_id, label, recipient_name, phone, full_address, city, province, postal_code, is_default)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING `+addressCols,
		userID, a.Label, a.RecipientName, a.Phone, a.FullAddress, a.City, a.Province, a.PostalCode, a.IsDefault))
	if err != nil {
		return nil, err
	}
	return created, tx.Commit(ctx)
}

// Update replaces an address owned by userID. When is_default=true the
// user's other addresses are unset in the same transaction. Returns
// ErrNotFound when the address does not exist or belongs to another user.
func (r *AddressRepo) Update(ctx context.Context, userID, id string, a *model.Address) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if a.IsDefault {
		if _, err := tx.Exec(ctx,
			`UPDATE addresses SET is_default = false, updated_at = now() WHERE user_id = $1`, userID); err != nil {
			return err
		}
	}

	tag, err := tx.Exec(ctx,
		`UPDATE addresses
		 SET label = $3, recipient_name = $4, phone = $5, full_address = $6,
		     city = $7, province = $8, postal_code = $9, is_default = $10, updated_at = now()
		 WHERE id = $1 AND user_id = $2`,
		id, userID, a.Label, a.RecipientName, a.Phone, a.FullAddress, a.City, a.Province, a.PostalCode, a.IsDefault)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

// Delete removes an address owned by userID. Returns ErrNotFound when it
// does not exist or belongs to another user.
func (r *AddressRepo) Delete(ctx context.Context, userID, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM addresses WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetDefault marks the address as default, unsetting all the user's other
// addresses in the same transaction (PRD C.7, S.13). Returns ErrNotFound
// when the address does not exist or belongs to another user.
func (r *AddressRepo) SetDefault(ctx context.Context, userID, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`UPDATE addresses SET is_default = false, updated_at = now() WHERE user_id = $1`, userID); err != nil {
		return err
	}

	tag, err := tx.Exec(ctx,
		`UPDATE addresses SET is_default = true, updated_at = now() WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}
