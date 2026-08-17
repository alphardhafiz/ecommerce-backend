package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"ecommerce/server/internal/model"
)

var (
	ErrNotFound   = errors.New("user not found")
	ErrEmailTaken = errors.New("email already exists")
)

const userColumns = "id, name, email, password_hash, role, status, phone, created_at, updated_at"

type UserRepo struct {
	pool *pgxpool.Pool
}

func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

func (r *UserRepo) Create(ctx context.Context, name, email, passwordHash string) (*model.User, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO users (name, email, password_hash)
		 VALUES ($1, $2, $3)
		 RETURNING `+userColumns,
		name, email, passwordHash)

	u, err := scanUser(row)
	if err != nil {
		return nil, mapUserError(err)
	}
	return u, nil
}

func (r *UserRepo) FindByID(ctx context.Context, id string) (*model.User, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = $1`, id)

	u, err := scanUser(row)
	if err != nil {
		return nil, mapUserError(err)
	}
	return u, nil
}

func (r *UserRepo) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM users WHERE email = $1`, email)

	u, err := scanUser(row)
	if err != nil {
		return nil, mapUserError(err)
	}
	return u, nil
}

func (r *UserRepo) UpdatePassword(ctx context.Context, id, passwordHash string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`,
		id, passwordHash)
	return err
}

func (r *UserRepo) Update(ctx context.Context, id, name string, phone *string) (*model.User, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE users
		 SET name = $2, phone = $3, updated_at = now()
		 WHERE id = $1
		 RETURNING `+userColumns,
		id, name, phone)

	u, err := scanUser(row)
	if err != nil {
		return nil, mapUserError(err)
	}
	return u, nil
}

// List returns users filtered by status (empty = all) with pagination and the
// total count matching the filter.
func (r *UserRepo) List(ctx context.Context, status string, limit, offset int) ([]*model.User, int64, error) {
	where := ""
	args := []any{}
	if status != "" {
		args = append(args, status)
		where = "WHERE status = $1"
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM users `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	query := fmt.Sprintf(`SELECT `+userColumns+` FROM users %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args))
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var users []*model.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		users = append(users, u)
	}
	return users, total, rows.Err()
}

func (r *UserRepo) UpdateStatus(ctx context.Context, id, status string) (*model.User, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE users
		 SET status = $2, updated_at = now()
		 WHERE id = $1
		 RETURNING `+userColumns,
		id, status)

	u, err := scanUser(row)
	if err != nil {
		return nil, mapUserError(err)
	}
	return u, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (*model.User, error) {
	u := &model.User{}
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.Role, &u.Status, &u.Phone, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func mapUserError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrEmailTaken
	}
	return err
}
