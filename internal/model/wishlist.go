package model

import "time"

// WishlistItem is a user's wishlist entry joined with the current product
// state (PRD C.5: list includes product status for UI indicators).
type WishlistItem struct {
	ProductID    string
	ProductName  string
	Price        int64
	Stock        int
	IsActive     bool
	InStock      bool
	PrimaryImage *string
	AddedAt      time.Time
}
