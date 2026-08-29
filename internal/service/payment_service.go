package service

import (
	"context"
	"encoding/json"
	"strings"

	"ecommerce/server/internal/model"
	"ecommerce/server/internal/payment"
	"ecommerce/server/internal/repository"
)

// midtransStatusMap maps Midtrans transaction_status to the internal
// payments.status values (PRD C.10). Unknown statuses are logged and
// skipped, not mapped to a final state.
var midtransStatusMap = map[string]string{
	"settlement": "SUCCESS",
	"capture":    "SUCCESS",
	"deny":       "FAILED",
	"failure":    "FAILED",
	"expire":     "EXPIRED",
	"cancel":     "CANCELLED",
	"pending":    "PENDING",
}

type PaymentService struct {
	payments *repository.PaymentRepo
	orders   *repository.OrderRepo
}

func NewPaymentService(payments *repository.PaymentRepo, orders *repository.OrderRepo) *PaymentService {
	return &PaymentService{payments: payments, orders: orders}
}

// GetPayment returns the payment of a user's order. Returns ErrForbidden
// for another user's order (PRD S.6) and ErrNotFound for a missing order
// or payment.
func (s *PaymentService) GetPayment(ctx context.Context, userID, orderID string) (*model.Payment, error) {
	order, err := s.orders.GetByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.UserID != userID {
		return nil, ErrForbidden
	}
	return s.payments.GetByOrderID(ctx, orderID)
}

// ProcessNotification applies a verified webhook notification
// idempotently: logs every delivery, then updates the payment status only
// when it changes. Returns the mapped internal status ("" when unknown).
// The order transition / stock restore on EXPIRED/CANCELLED is handled in
// T5 (PRD F.3) on top of the payment status this lands here.
func (s *PaymentService) ProcessNotification(ctx context.Context, n payment.Notification, raw json.RawMessage) (string, error) {
	mapped, ok := midtransStatusMap[strings.ToLower(n.TransactionStatus)]
	if !ok {
		return "", nil
	}
	if _, err := s.payments.ProcessNotification(ctx, n.OrderID, raw, mapped); err != nil {
		return "", err
	}
	return mapped, nil
}
