package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ecommerce/server/internal/model"
)

var (
	ErrCartEmpty         = errors.New("cart is empty")
	ErrInvalidAddress    = errors.New("address not found or not owned by user")
	ErrProductNotFound   = errors.New("product not found")
	ErrProductInactive   = errors.New("product inactive or deleted")
	ErrInsufficientStock = errors.New("insufficient stock")
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
