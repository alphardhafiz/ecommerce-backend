package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PasswordResetTokenRepo struct {
	pool *pgxpool.Pool
}

func NewPasswordResetTokenRepo(pool *pgxpool.Pool) *PasswordResetTokenRepo {
	return &PasswordResetTokenRepo{pool: pool}
}

func (r *PasswordResetTokenRepo) Create(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
		 VALUES ($1, $2, $3)`,
		userID, tokenHash, expiresAt)
	return err
}
