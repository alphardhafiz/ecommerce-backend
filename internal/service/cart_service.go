package service

import (
	"context"

	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
)

type CartService struct {
	carts *repository.CartRepo
}

func NewCartService(carts *repository.CartRepo) *CartService {
	return &CartService{carts: carts}
}

// Get returns the user's cart (lazily created) with items and the total of
// available items only. Unavailable items (inactive/soft-deleted products)
// stay listed with is_available=false but are excluded from the total
// (PRD C.6).
func (s *CartService) Get(ctx context.Context, userID string) (*model.Cart, []*model.CartItem, int64, error) {
	cart, err := s.carts.GetOrCreate(ctx, userID)
	if err != nil {
		return nil, nil, 0, err
	}
	items, err := s.carts.ListItems(ctx, cart.ID)
	if err != nil {
		return nil, nil, 0, err
	}

	var total int64
	for _, item := range items {
		if item.IsAvailable {
			total += item.Subtotal
		}
	}
	return cart, items, total, nil
}
