package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrResetTokenInvalid = errors.New("reset token invalid or expired")

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

// Consume marks a single-use reset token as used and returns its owner. It
// fails with ErrResetTokenInvalid for unknown, already-used, or expired tokens
// (uniform, anti-enumeration). FOR UPDATE serializes concurrent use so a token
// can only be consumed once.
func (r *PasswordResetTokenRepo) Consume(ctx context.Context, tokenHash string) (string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var userID string
	var usedAt *time.Time
	var expiresAt time.Time
	err = tx.QueryRow(ctx,
		`SELECT user_id, used_at, expires_at FROM password_reset_tokens WHERE token_hash = $1 FOR UPDATE`,
		tokenHash).Scan(&userID, &usedAt, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrResetTokenInvalid
	}
	if err != nil {
		return "", err
	}
	if usedAt != nil || time.Now().After(expiresAt) {
		return "", ErrResetTokenInvalid
	}

	if _, err := tx.Exec(ctx,
		`UPDATE password_reset_tokens SET used_at = now() WHERE token_hash = $1`, tokenHash); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return userID, nil
}
