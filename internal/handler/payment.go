package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"ecommerce/server/internal/payment"
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
}

func NewPayment(g PaymentGateway) *Payment {
	return &Payment{gateway: g}
}

// Webhook receives Midtrans notifications (PRD F.2). It never trusts the
// raw payload: signature must match (else 401) and the server-to-server
// status check must succeed (else 500, Midtrans retries). State mutations
// (idempotency, payment_notifications, order/payment status) land in T4/T5.
func (p *Payment) Webhook(w http.ResponseWriter, r *http.Request) {
	var n payment.Notification
	if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
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

	respondJSON(w, http.StatusOK, map[string]any{"success": true, "data": nil})
}
