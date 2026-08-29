package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ecommerce/server/internal/model"
)

var ErrPaymentNotFound = errors.New("payment not found")

type PaymentRepo struct {
	pool *pgxpool.Pool
}

func NewPaymentRepo(pool *pgxpool.Pool) *PaymentRepo {
	return &PaymentRepo{pool: pool}
}

const paymentCols = `id, order_id, midtrans_order_id, status, amount::bigint, payment_type, paid_at, created_at, updated_at`

// GetByOrderID returns the payment of an order. Every checkout creates one
// (T2), so this should always exist; returns ErrPaymentNotFound otherwise.
func (r *PaymentRepo) GetByOrderID(ctx context.Context, orderID string) (*model.Payment, error) {
	p := &model.Payment{}
	if err := r.pool.QueryRow(ctx,
		`SELECT `+paymentCols+` FROM payments WHERE order_id = $1`, orderID).
		Scan(&p.ID, &p.OrderID, &p.MidtransOrderID, &p.Status, &p.Amount, &p.PaymentType, &p.PaidAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrPaymentNotFound
		}
		return nil, err
	}
	return p, nil
}

// ProcessNotification applies a webhook delivery in one transaction (PRD
// F.2, F.3, C.10): audit-log every delivery, then move payment + order
// together — never one without the other.
//
//   - payment PENDING + SUCCESS: payment SUCCESS + paid_at, order PENDING→PAID
//   - payment PENDING + EXPIRED/CANCELLED: payment set, order PENDING→EXPIRED/
//     CANCELLED and stock restored (PRD S.14)
//   - payment PENDING + FAILED: payment FAILED, order stays PENDING (retry)
//   - duplicate or late delivery (payment already at target, or already past
//     PENDING): no-op — "set to final state" is idempotent, stock never
//     returns twice
//
// The order transition is guarded by WHERE status = 'PENDING', so even a
// concurrently-paid order is never double-moved.
func (r *PaymentRepo) ProcessNotification(ctx context.Context, midtransOrderID string, raw json.RawMessage, status string) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var paymentID, current, orderID string
	if err := tx.QueryRow(ctx,
		`SELECT id, status, order_id FROM payments WHERE midtrans_order_id = $1 FOR UPDATE`, midtransOrderID).
		Scan(&paymentID, &current, &orderID); err != nil {
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

	// Idempotent no-op: already at target (duplicate delivery), or the
	// payment already left PENDING (final state; a late webhook must not
	// downgrade it, PRD S.7 anomaly).
	if current == status || current != "PENDING" {
		return false, tx.Commit(ctx)
	}

	switch status {
	case "SUCCESS":
		if _, err := tx.Exec(ctx,
			`UPDATE payments SET status = 'SUCCESS', paid_at = now(), updated_at = now() WHERE id = $1`,
			paymentID); err != nil {
			return false, err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE orders SET status = 'PAID', updated_at = now() WHERE id = $1 AND status = 'PENDING'`,
			orderID); err != nil {
			return false, err
		}
	case "FAILED":
		if _, err := tx.Exec(ctx,
			`UPDATE payments SET status = 'FAILED', updated_at = now() WHERE id = $1`,
			paymentID); err != nil {
			return false, err
		}
	default: // EXPIRED, CANCELLED: order expires/cancels, stock returns
		if _, err := tx.Exec(ctx,
			`UPDATE payments SET status = $2, updated_at = now() WHERE id = $1`,
			paymentID, status); err != nil {
			return false, err
		}
		tag, err := tx.Exec(ctx,
			`UPDATE orders SET status = $2, updated_at = now() WHERE id = $1 AND status = 'PENDING'`,
			orderID, status)
		if err != nil {
			return false, err
		}
		if tag.RowsAffected() == 1 {
			if _, err := tx.Exec(ctx,
				`UPDATE products p
				 SET stock = p.stock + oi.quantity, updated_at = now()
				 FROM order_items oi
				 WHERE oi.order_id = $1 AND p.id = oi.product_id`, orderID); err != nil {
				return false, err
			}
		}
	}

	return true, tx.Commit(ctx)
}
