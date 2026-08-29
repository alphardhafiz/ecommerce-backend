package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"ecommerce/server/internal/middleware"
	"ecommerce/server/internal/payment"
	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/service"
)

// PaymentGateway is the Midtrans surface the webhook needs (PRD F.2):
// signature verification + server-to-server status double-check. The real
// payment.Client satisfies it; tests can stub it.
type PaymentGateway interface {
	VerifySignature(n payment.Notification) bool
	GetStatus(ctx context.Context, orderID string) (*payment.Status, error)
}

type Payment struct {
	gateway PaymentGateway
	svc     *service.PaymentService
}

func NewPayment(g PaymentGateway, svc *service.PaymentService) *Payment {
	return &Payment{gateway: g, svc: svc}
}

// Webhook receives Midtrans notifications (PRD F.2). It never trusts the
// raw payload: signature must match (else 401) and the server-to-server
// status check must succeed (else 500, Midtrans retries). Every valid
// delivery is then applied idempotently (audit log + status update only
// when it changes, PRD C.10); duplicates still get 200.
func (p *Payment) Webhook(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}
	var n payment.Notification
	if err := json.Unmarshal(raw, &n); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}
	if n.OrderID == "" || n.TransactionStatus == "" {
		respondError(w, http.StatusBadRequest, "Missing required fields", "INVALID_REQUEST", nil)
		return
	}

	if !p.gateway.VerifySignature(n) {
		respondError(w, http.StatusUnauthorized, "Invalid signature", "INVALID_SIGNATURE", nil)
		return
	}

	st, err := p.gateway.GetStatus(r.Context(), n.OrderID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Status check failed", "STATUS_CHECK_FAILED", nil)
		return
	}
	if st.TransactionStatus != "" && st.TransactionStatus != n.TransactionStatus {
		slog.Warn("webhook payload differs from Midtrans status check",
			"order_id", n.OrderID, "payload", n.TransactionStatus, "server", st.TransactionStatus)
	}

	mapped, err := p.svc.ProcessNotification(r.Context(), n, raw)
	if err != nil {
		if errors.Is(err, repository.ErrPaymentNotFound) {
			// Signature is valid but we don't know this payment: log the
			// anomaly (PRD S.7) and still reply 200 so Midtrans stops
			// retrying this delivery.
			slog.Warn("webhook for unknown payment", "order_id", n.OrderID)
			respondJSON(w, http.StatusOK, map[string]any{"success": true, "data": nil})
			return
		}
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	if mapped == "" {
		slog.Warn("webhook with unmapped transaction_status", "order_id", n.OrderID, "status", n.TransactionStatus)
	}

	respondJSON(w, http.StatusOK, map[string]any{"success": true, "data": nil})
}

// Get returns the payment status of the user's own order (PRD E,
// ownership per PRD S.6).
func (p *Payment) Get(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	pay, err := p.svc.GetPayment(r.Context(), claims.UserID, r.PathValue("id"))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrForbidden):
			respondError(w, http.StatusForbidden, "Forbidden", "FORBIDDEN", nil)
		case errors.Is(err, repository.ErrNotFound), errors.Is(err, repository.ErrPaymentNotFound):
			respondError(w, http.StatusNotFound, "Payment not found", "NOT_FOUND", nil)
		default:
			respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		}
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"order_id":     pay.OrderID,
			"status":       pay.Status,
			"amount":       pay.Amount,
			"payment_type": pay.PaymentType,
			"paid_at":      pay.PaidAt,
		},
	})
}
