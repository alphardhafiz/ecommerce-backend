package service

import (
	"context"
	"encoding/json"
	"strings"

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
}

func NewPaymentService(payments *repository.PaymentRepo) *PaymentService {
	return &PaymentService{payments: payments}
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
