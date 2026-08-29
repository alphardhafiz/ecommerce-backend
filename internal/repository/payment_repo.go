package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrPaymentNotFound = errors.New("payment not found")

type PaymentRepo struct {
	pool *pgxpool.Pool
}

func NewPaymentRepo(pool *pgxpool.Pool) *PaymentRepo {
	return &PaymentRepo{pool: pool}
}

// ProcessNotification logs a webhook delivery and idempotently applies the
// mapped payment status, all in one transaction (PRD F.2, C.10):
//  1. lock the payments row (by midtrans_order_id)
//  2. insert the payment_notifications audit row — every delivery is logged,
//     duplicates included
//  3. skip the status update when it already matches (duplicate delivery →
//     no-op, caller still replies 200)
//
// Returns changed=true when the payment status was actually updated.
func (r *PaymentRepo) ProcessNotification(ctx context.Context, midtransOrderID string, raw json.RawMessage, status string) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var paymentID, current string
	if err := tx.QueryRow(ctx,
		`SELECT id, status FROM payments WHERE midtrans_order_id = $1 FOR UPDATE`, midtransOrderID).
		Scan(&paymentID, &current); err != nil {
		if err == pgx.ErrNoRows {
			return false, ErrPaymentNotFound
		}
		return false, err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO payment_notifications (payment_id, raw_payload, transaction_status)
		 VALUES ($1, $2, $3)`,
		paymentID, raw, status); err != nil {
		return false, err
	}

	if current == status {
		return false, tx.Commit(ctx)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE payments SET status = $2, updated_at = now() WHERE id = $1`,
		paymentID, status); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
