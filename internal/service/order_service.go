package service

import (
	"context"

	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
)

type OrderService struct {
	orders *repository.OrderRepo
}

func NewOrderService(orders *repository.OrderRepo) *OrderService {
	return &OrderService{orders: orders}
}

// Checkout creates a PENDING order. cart_item_ids must be non-empty and
// address_id must be provided; financial values never come from the request
// body (PRD C.8).
func (s *OrderService) Checkout(ctx context.Context, userID string, cartItemIDs []string, addressID string) (*model.Order, error) {
	if len(cartItemIDs) == 0 {
		return nil, &ValidationError{Errors: []FieldError{{Field: "cart_item_ids", Message: "At least one cart item is required"}}}
	}
	if addressID == "" {
		return nil, &ValidationError{Errors: []FieldError{{Field: "address_id", Message: "Address is required"}}}
	}
	return s.orders.Checkout(ctx, userID, cartItemIDs, addressID)
}
