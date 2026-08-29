package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ecommerce/server/internal/model"
)

var (
	ErrCartEmpty               = errors.New("cart is empty")
	ErrInvalidAddress          = errors.New("address not found or not owned by user")
	ErrProductNotFound         = errors.New("product not found")
	ErrProductInactive         = errors.New("product inactive or deleted")
	ErrInsufficientStock       = errors.New("insufficient stock")
	ErrOrderNotCancellable     = errors.New("order cannot be cancelled")
	ErrInvalidStatusTransition = errors.New("invalid order status transition")
)

type OrderRepo struct {
	pool *pgxpool.Pool
}

func NewOrderRepo(pool *pgxpool.Pool) *OrderRepo {
	return &OrderRepo{pool: pool}
}

// Checkout creates a PENDING order from the user's selected cart items in a
// single transaction (PRD C.8, G):
//  1. snapshot the address (not a FK)
//  2. re-validate products (exists, active, not soft-deleted) and lock their
//     rows with SELECT ... FOR UPDATE in consistent id order (deadlock-free)
//  3. re-read price from DB — request bodies carry no financial values
//  4. decrement stock, insert order + order_items (snapshots), delete the
//     checked-out cart lines
//
// Returns ErrCartEmpty when no (valid) cart line matches, ErrInvalidAddress
// when the address is missing or belongs to another user, ErrProductInactive,
// ErrInsufficientStock, ErrProductNotFound.
func (r *OrderRepo) Checkout(ctx context.Context, userID string, cartItemIDs []string, addressID string) (*model.Order, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var recipientName, phone, shippingAddress string
	if err := tx.QueryRow(ctx,
		`SELECT recipient_name, phone, full_address
		 FROM addresses
		 WHERE id = $1 AND user_id = $2`, addressID, userID).Scan(&recipientName, &phone, &shippingAddress); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrInvalidAddress
		}
		return nil, err
	}

	rows, err := tx.Query(ctx,
		`SELECT ci.id, ci.product_id, ci.quantity
		 FROM cart_items ci
		 JOIN carts c ON c.id = ci.cart_id
		 WHERE c.user_id = $1 AND ci.id = ANY($2)`, userID, cartItemIDs)
	if err != nil {
		return nil, err
	}

	type cartLine struct {
		itemID    string
		productID string
		qty       int
	}
	var lines []cartLine
	for rows.Next() {
		var l cartLine
		if err := rows.Scan(&l.itemID, &l.productID, &l.qty); err != nil {
			rows.Close()
			return nil, err
		}
		lines = append(lines, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(lines) == 0 || len(lines) != len(cartItemIDs) {
		return nil, ErrCartEmpty
	}

	productIDs := make([]string, len(lines))
	for i, l := range lines {
		productIDs[i] = l.productID
	}

	// Lock product rows in consistent order to avoid deadlocks (PRD G).
	prodRows, err := tx.Query(ctx,
		`SELECT id, name, price::bigint, stock, is_active, deleted_at IS NOT NULL
		 FROM products
		 WHERE id = ANY($1)
		 ORDER BY id
		 FOR UPDATE`, productIDs)
	if err != nil {
		return nil, err
	}
	defer prodRows.Close()

	type productLock struct {
		id        string
		name      string
		price     int64
		stock     int
		isActive  bool
		isDeleted bool
	}
	locked := make(map[string]*productLock, len(productIDs))
	for prodRows.Next() {
		p := &productLock{}
		if err := prodRows.Scan(&p.id, &p.name, &p.price, &p.stock, &p.isActive, &p.isDeleted); err != nil {
			return nil, err
		}
		locked[p.id] = p
	}
	if err := prodRows.Err(); err != nil {
		return nil, err
	}
	if len(locked) != len(productIDs) {
		return nil, ErrProductNotFound
	}

	var total int64
	type item struct {
		line  cartLine
		prod  *productLock
		price int64
	}
	var items []item
	for _, l := range lines {
		p, ok := locked[l.productID]
		if !ok {
			return nil, ErrProductNotFound
		}
		if p.isDeleted || !p.isActive {
			return nil, &ProductError{Code: "PRODUCT_INACTIVE", ProductID: p.id, ProductName: p.name}
		}
		if p.stock < l.qty {
			return nil, &ProductError{Code: "PRODUCT_OUT_OF_STOCK", ProductID: p.id, ProductName: p.name}
		}
		total += p.price * int64(l.qty)
		items = append(items, item{line: l, prod: p, price: p.price})
	}

	for _, it := range items {
		if _, err := tx.Exec(ctx,
			`UPDATE products SET stock = stock - $1, updated_at = now() WHERE id = $2`,
			it.line.qty, it.prod.id); err != nil {
			return nil, err
		}
	}

	order := &model.Order{
		UserID:          userID,
		Status:          "PENDING",
		TotalAmount:     total,
		RecipientName:   recipientName,
		Phone:           phone,
		ShippingAddress: shippingAddress,
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO orders (user_id, status, total_amount, recipient_name, phone, shipping_address, expired_at)
		 VALUES ($1, 'PENDING', $2, $3, $4, $5, now() + interval '60 minutes')
		 RETURNING id, created_at, updated_at`,
		order.UserID, order.TotalAmount, order.RecipientName, order.Phone, order.ShippingAddress).Scan(&order.ID, &order.CreatedAt, &order.UpdatedAt); err != nil {
		return nil, err
	}

	for _, it := range items {
		if _, err := tx.Exec(ctx,
			`INSERT INTO order_items (order_id, product_id, product_name, price, quantity, subtotal)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			order.ID, it.prod.id, it.prod.name, it.price, it.line.qty, it.price*int64(it.line.qty)); err != nil {
			return nil, err
		}
	}

	cartItemIDsForDelete := make([]string, len(items))
	for i, it := range items {
		cartItemIDsForDelete[i] = it.line.itemID
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM cart_items WHERE id = ANY($1)`, cartItemIDsForDelete); err != nil {
		return nil, err
	}

	return order, tx.Commit(ctx)
}

const orderCols = `id, user_id, status, total_amount::bigint, recipient_name, phone, shipping_address, expired_at, created_at, updated_at`

func scanOrder(row pgx.Row) (*model.Order, error) {
	o := &model.Order{}
	if err := row.Scan(&o.ID, &o.UserID, &o.Status, &o.TotalAmount, &o.RecipientName, &o.Phone, &o.ShippingAddress, &o.ExpiredAt, &o.CreatedAt, &o.UpdatedAt); err != nil {
		return nil, err
	}
	return o, nil
}

// Cancel transitions a PENDING order to CANCELLED and restores stock for
// every order_item in the same transaction (PRD C.9, S.14). The status
// check is atomic (UPDATE ... WHERE status = 'PENDING'), so a concurrent
// transition (e.g. payment webhook) wins and this returns
// ErrOrderNotCancellable.
func (r *OrderRepo) Cancel(ctx context.Context, orderID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE orders SET status = 'CANCELLED', updated_at = now()
		 WHERE id = $1 AND status = 'PENDING'`, orderID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrOrderNotCancellable
	}

	if _, err := tx.Exec(ctx,
		`UPDATE products p
		 SET stock = p.stock + oi.quantity, updated_at = now()
		 FROM order_items oi
		 WHERE oi.order_id = $1 AND p.id = oi.product_id`, orderID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ListAll returns every user's orders (admin view) filtered by status
// (empty = all), newest first, with the total count for pagination meta.
func (r *OrderRepo) ListAll(ctx context.Context, status string, limit, offset int) ([]*model.Order, int64, error) {
	where := ""
	args := []any{}
	if status != "" {
		args = append(args, status)
		where = "WHERE status = $1"
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM orders `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx,
		fmt.Sprintf(`SELECT `+orderCols+` FROM orders %s
		 ORDER BY created_at DESC
		 LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var orders []*model.Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, 0, err
		}
		orders = append(orders, o)
	}
	return orders, total, rows.Err()
}

// UpdateStatus transitions an order from fromStatus to toStatus, restoring
// stock when the new status is CANCELLED (PRD S.14) — all in one
// transaction. The fromStatus guard makes concurrent transitions (payment
// webhook, scheduled job) win; a guard miss returns
// ErrInvalidStatusTransition.
func (r *OrderRepo) UpdateStatus(ctx context.Context, orderID, fromStatus, toStatus string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE orders SET status = $3, updated_at = now()
		 WHERE id = $1 AND status = $2`, orderID, fromStatus, toStatus)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidStatusTransition
	}

	if toStatus == "CANCELLED" {
		if _, err := tx.Exec(ctx,
			`UPDATE products p
			 SET stock = p.stock + oi.quantity, updated_at = now()
			 FROM order_items oi
			 WHERE oi.order_id = $1 AND p.id = oi.product_id`, orderID); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// List returns the user's orders newest first, with the total count for
// pagination meta.
func (r *OrderRepo) List(ctx context.Context, userID string, limit, offset int) ([]*model.Order, int64, error) {
	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM orders WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := r.pool.Query(ctx,
		`SELECT `+orderCols+` FROM orders
		 WHERE user_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var orders []*model.Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, 0, err
		}
		orders = append(orders, o)
	}
	return orders, total, rows.Err()
}

// GetByID returns an order by id regardless of owner (user_id is checked by
// the service for ownership, PRD S.6). Returns ErrNotFound when it does not
// exist.
func (r *OrderRepo) GetByID(ctx context.Context, orderID string) (*model.Order, error) {
	o, err := scanOrder(r.pool.QueryRow(ctx,
		`SELECT `+orderCols+` FROM orders WHERE id = $1`, orderID))
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return o, nil
}

// ListItemsByOrderIDs returns order_items for the given order ids, grouped
// by order_id (single query, no N+1).
func (r *OrderRepo) ListItemsByOrderIDs(ctx context.Context, orderIDs []string) (map[string][]*model.OrderItem, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, order_id, product_id, product_name, price::bigint, quantity, subtotal::bigint
		 FROM order_items
		 WHERE order_id = ANY($1)
		 ORDER BY created_at`, orderIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string][]*model.OrderItem)
	for rows.Next() {
		it := &model.OrderItem{}
		if err := rows.Scan(&it.ID, &it.OrderID, &it.ProductID, &it.ProductName, &it.Price, &it.Quantity, &it.Subtotal); err != nil {
			return nil, err
		}
		out[it.OrderID] = append(out[it.OrderID], it)
	}
	return out, rows.Err()
}
