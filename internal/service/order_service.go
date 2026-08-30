package service

import (
	"context"

	"ecommerce/server/internal/model"
	"ecommerce/server/internal/payment"
	"ecommerce/server/internal/repository"
)

// orderTransitions encodes the admin state machine (PRD C.9). PENDING is
// handled by webhook/job/user-cancel, never by the admin PATCH. Final
// states (COMPLETED/CANCELLED/EXPIRED) have no outgoing edges.
var orderTransitions = map[string]map[string]bool{
	"PAID":       {"PROCESSING": true, "CANCELLED": true},
	"PROCESSING": {"SHIPPED": true},
	"SHIPPED":    {"COMPLETED": true},
}

type OrderService struct {
	orders   *repository.OrderRepo
	payments *repository.PaymentRepo
}

func NewOrderService(orders *repository.OrderRepo, payments *repository.PaymentRepo) *OrderService {
	return &OrderService{orders: orders, payments: payments}
}

// Checkout creates a PENDING order and its Midtrans payment (PRD F.1).
// cart_item_ids must be non-empty and address_id must be provided;
// financial values never come from the request body (PRD C.8).
func (s *OrderService) Checkout(ctx context.Context, userID string, cartItemIDs []string, addressID string) (*model.Order, *payment.Transaction, error) {
	if len(cartItemIDs) == 0 {
		return nil, nil, &ValidationError{Errors: []FieldError{{Field: "cart_item_ids", Message: "At least one cart item is required"}}}
	}
	if addressID == "" {
		return nil, nil, &ValidationError{Errors: []FieldError{{Field: "address_id", Message: "Address is required"}}}
	}
	return s.orders.Checkout(ctx, userID, cartItemIDs, addressID)
}

// List returns the user's orders (newest first) with items grouped per
// order, plus the total count for pagination meta.
func (s *OrderService) List(ctx context.Context, userID string, limit, offset int) ([]*model.Order, map[string][]*model.OrderItem, int64, error) {
	orders, total, err := s.orders.List(ctx, userID, limit, offset)
	if err != nil {
		return nil, nil, 0, err
	}
	if len(orders) == 0 {
		return orders, map[string][]*model.OrderItem{}, 0, nil
	}

	ids := make([]string, len(orders))
	for i, o := range orders {
		ids[i] = o.ID
	}
	items, err := s.orders.ListItemsByOrderIDs(ctx, ids)
	if err != nil {
		return nil, nil, 0, err
	}
	return orders, items, total, nil
}

// Get returns an order with its items and payment (nil when the order has
// none). Returns ErrForbidden when the order belongs to another user
// (PRD S.6).
func (s *OrderService) Get(ctx context.Context, userID, orderID string) (*model.Order, []*model.OrderItem, *model.Payment, error) {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return nil, nil, nil, err
	}
	if order.UserID != userID {
		return nil, nil, nil, ErrForbidden
	}
	items, err := s.orders.ListItemsByOrderIDs(ctx, []string{order.ID})
	if err != nil {
		return nil, nil, nil, err
	}
	payment, err := s.payments.GetByOrderID(ctx, order.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	return order, items[order.ID], payment, nil
}

// Cancel cancels the user's PENDING order. Returns ErrForbidden for another
// user's order (PRD S.6) and ErrOrderNotCancellable when the status is no
// longer PENDING (PRD C.9, S.12).
func (s *OrderService) Cancel(ctx context.Context, userID, orderID string) error {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return err
	}
	if order.UserID != userID {
		return ErrForbidden
	}
	return s.orders.Cancel(ctx, orderID)
}

// ListAll returns every user's orders (admin view) with items grouped per
// order and the total count for pagination meta.
func (s *OrderService) ListAll(ctx context.Context, status string, limit, offset int) ([]*model.Order, map[string][]*model.OrderItem, int64, error) {
	orders, total, err := s.orders.ListAll(ctx, status, limit, offset)
	if err != nil {
		return nil, nil, 0, err
	}
	if len(orders) == 0 {
		return orders, map[string][]*model.OrderItem{}, 0, nil
	}

	ids := make([]string, len(orders))
	for i, o := range orders {
		ids[i] = o.ID
	}
	items, err := s.orders.ListItemsByOrderIDs(ctx, ids)
	if err != nil {
		return nil, nil, 0, err
	}
	return orders, items, total, nil
}

// UpdateStatus applies an admin status transition (PRD C.9). Returns
// ErrInvalidStatusTransition when the transition is not allowed or the
// status changed concurrently.
func (s *OrderService) UpdateStatus(ctx context.Context, orderID, toStatus string) error {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return err
	}
	if !orderTransitions[order.Status][toStatus] {
		return repository.ErrInvalidStatusTransition
	}
	return s.orders.UpdateStatus(ctx, orderID, order.Status, toStatus)
}
