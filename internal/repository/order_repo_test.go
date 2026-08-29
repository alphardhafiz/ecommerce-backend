package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// seedOrderForExpire creates a user, product (stock 5, decremented to 3), a
// PENDING order with an order_item (qty 2) and its PENDING payment; returns
// orderID + productID.
func seedOrderForExpire(t *testing.T, pool *pgxpool.Pool, email string, pastDue bool) (string, string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx,
		`INSERT INTO users (name, email, password_hash, role, status)
		 VALUES ('Budi', $1, 'hash', 'user', 'active')
		 ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name`,
		email); err != nil {
		t.Fatal(err)
	}
	var userID string
	if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}

	var prodID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO products (name, price, stock, is_active)
		 VALUES ('Produk Expire', 50000, 5, true)
		 RETURNING id`).Scan(&prodID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE products SET stock = 3 WHERE id = $1`, prodID); err != nil {
		t.Fatal(err)
	}

	expiredAt := "now() + interval '1 hour'"
	if pastDue {
		expiredAt = "now() - interval '1 minute'"
	}
	var orderID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO orders (user_id, status, total_amount, recipient_name, phone, shipping_address, expired_at)
		 VALUES ($1, 'PENDING', 100000, 'Budi', '08123456789', 'Jl. Merdeka No. 1', `+expiredAt+`)
		 RETURNING id`, userID).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO payments (order_id, midtrans_order_id, status, amount)
		 VALUES ($1, $2::text, 'PENDING', 100000)`, orderID, orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO order_items (order_id, product_id, product_name, price, quantity, subtotal)
		 VALUES ($1, $2, 'Produk Expire', 50000, 2, 100000)`, orderID, prodID); err != nil {
		t.Fatal(err)
	}
	return orderID, prodID
}

func cleanupOrderForExpire(t *testing.T, pool *pgxpool.Pool, orderID, prodID, email string) {
	t.Helper()
	ctx := context.Background()
	pool.Exec(ctx, `DELETE FROM payment_notifications WHERE payment_id IN (SELECT id FROM payments WHERE order_id = $1)`, orderID)
	pool.Exec(ctx, `DELETE FROM payments WHERE order_id = $1`, orderID)
	pool.Exec(ctx, `DELETE FROM order_items WHERE order_id = $1`, orderID)
	pool.Exec(ctx, `DELETE FROM orders WHERE id = $1`, orderID)
	pool.Exec(ctx, `DELETE FROM products WHERE id = $1`, prodID)
	pool.Exec(ctx, `DELETE FROM users WHERE email = $1`, email)
}

func TestOrderRepoExpirePending(t *testing.T) {
	ctx := context.Background()
	repo := NewOrderRepo(newTestPool(t))
	pool := repo.pool

	dueID, dueProd := seedOrderForExpire(t, pool, "expire-due@example.com", true)
	defer cleanupOrderForExpire(t, pool, dueID, dueProd, "expire-due@example.com")
	futureID, futureProd := seedOrderForExpire(t, pool, "expire-future@example.com", false)
	defer cleanupOrderForExpire(t, pool, futureID, futureProd, "expire-future@example.com")

	n, err := repo.ExpirePending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expired = %d, want 1 (only the overdue order)", n)
	}

	var orderStatus, payStatus string
	var stock int
	pool.QueryRow(ctx, `SELECT status FROM orders WHERE id = $1`, dueID).Scan(&orderStatus)
	pool.QueryRow(ctx, `SELECT status FROM payments WHERE order_id = $1`, dueID).Scan(&payStatus)
	pool.QueryRow(ctx, `SELECT stock FROM products WHERE id = $1`, dueProd).Scan(&stock)
	if orderStatus != "EXPIRED" || payStatus != "EXPIRED" {
		t.Errorf("order/payment = %s/%s, want EXPIRED/EXPIRED", orderStatus, payStatus)
	}
	if stock != 5 {
		t.Errorf("stock = %d, want restored 5", stock)
	}

	// not-yet-due order untouched
	pool.QueryRow(ctx, `SELECT status FROM orders WHERE id = $1`, futureID).Scan(&orderStatus)
	if orderStatus != "PENDING" {
		t.Errorf("future order = %s, want still PENDING", orderStatus)
	}

	// idempotent: second sweep expires nothing, stock not doubled
	n, err = repo.ExpirePending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("second sweep = %d, want 0", n)
	}
	pool.QueryRow(ctx, `SELECT stock FROM products WHERE id = $1`, dueProd).Scan(&stock)
	if stock != 5 {
		t.Errorf("stock after second sweep = %d, want still 5", stock)
	}
}
