package model

import "time"

type Cart struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CartItem is a cart line joined with current product state. Subtotal and
// availability are computed on the fly from the DB (PRD C.6), never stored.
type CartItem struct {
	ID          string
	UserID      string
	CartID      string
	ProductID   string
	Name        string
	Price       int64
	Stock       int
	Quantity    int
	Subtotal    int64
	IsAvailable bool
}
