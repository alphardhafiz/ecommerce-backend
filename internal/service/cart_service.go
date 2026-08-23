package service

import (
	"context"
	"errors"

	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
)

var ErrForbidden = errors.New("forbidden")

type CartService struct {
	carts    *repository.CartRepo
	products *repository.ProductRepo
}

func NewCartService(carts *repository.CartRepo, products *repository.ProductRepo) *CartService {
	return &CartService{carts: carts, products: products}
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

// AddItem adds a product to the user's cart (lazily created). Quantity is
// merged when the product is already present (PRD C.6). The product must
// exist and not be soft-deleted, else ErrNotFound.
func (s *CartService) AddItem(ctx context.Context, userID, productID string, quantity int) (*model.Cart, error) {
	if quantity <= 0 {
		return nil, &ValidationError{Errors: []FieldError{{Field: "quantity", Message: "Quantity must be greater than 0"}}}
	}

	p, err := s.products.FindByID(ctx, productID)
	if err != nil {
		return nil, err
	}
	if p.DeletedAt != nil {
		return nil, repository.ErrNotFound
	}

	cart, err := s.carts.GetOrCreate(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := s.carts.AddItem(ctx, cart.ID, productID, quantity); err != nil {
		return nil, err
	}
	return cart, nil
}

// UpdateQuantity sets a cart line's quantity with soft stock validation
// (PRD C.6: validated at request time; hard re-validation at checkout).
// Returns ErrForbidden when the line belongs to another user.
func (s *CartService) UpdateQuantity(ctx context.Context, userID, itemID string, quantity int) error {
	if quantity <= 0 {
		return &ValidationError{Errors: []FieldError{{Field: "quantity", Message: "Quantity must be greater than 0"}}}
	}

	item, err := s.carts.FindItem(ctx, itemID)
	if err != nil {
		return err
	}
	if item.UserID != userID {
		return ErrForbidden
	}
	if quantity > item.Stock {
		return &ValidationError{Errors: []FieldError{{Field: "quantity", Message: "Quantity exceeds available stock"}}}
	}
	return s.carts.UpdateQuantity(ctx, itemID, quantity)
}

// RemoveItem deletes a cart line. Returns ErrForbidden when the line belongs
// to another user.
func (s *CartService) RemoveItem(ctx context.Context, userID, itemID string) error {
	item, err := s.carts.FindItem(ctx, itemID)
	if err != nil {
		return err
	}
	if item.UserID != userID {
		return ErrForbidden
	}
	return s.carts.RemoveItem(ctx, itemID)
}

// Clear empties the user's cart.
func (s *CartService) Clear(ctx context.Context, userID string) error {
	cart, err := s.carts.GetOrCreate(ctx, userID)
	if err != nil {
		return err
	}
	return s.carts.Clear(ctx, cart.ID)
}
