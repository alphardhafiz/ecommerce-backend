package handler

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ecommerce/server/internal/payment"
)

const webhookServerKey = "SB-Mid-server-test-key"

// stubGateway simulates Midtrans for webhook tests: signature verified with
// the known key, status check returns the configured status or error.
type stubGateway struct {
	status *payment.Status
	err    error
}

func (s stubGateway) VerifySignature(n payment.Notification) bool {
	sum := sha512.Sum512([]byte(n.OrderID + n.StatusCode + n.GrossAmount + webhookServerKey))
	return strings.EqualFold(hex.EncodeToString(sum[:]), n.SignatureKey)
}

func (s stubGateway) GetStatus(ctx context.Context, orderID string) (*payment.Status, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.status, nil
}

func signedWebhookBody(orderID, status string) string {
	n := payment.Notification{
		OrderID:           orderID,
		StatusCode:        "200",
		GrossAmount:       "100000.00",
		TransactionStatus: status,
	}
	sum := sha512.Sum512([]byte(n.OrderID + n.StatusCode + n.GrossAmount + webhookServerKey))
	n.SignatureKey = hex.EncodeToString(sum[:])
	b, _ := json.Marshal(n)
	return string(b)
}

func webhookRequest(t *testing.T, gw PaymentGateway, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/payments/webhook", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	NewPayment(gw).Webhook(rec, req)
	return rec
}

func TestWebhookValidSignature(t *testing.T) {
	gw := stubGateway{status: &payment.Status{TransactionStatus: "settlement"}}
	rec := webhookRequest(t, gw, signedWebhookBody("order-1", "settlement"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
}

func TestWebhookInvalidSignature(t *testing.T) {
	gw := stubGateway{status: &payment.Status{TransactionStatus: "settlement"}}
	body := signedWebhookBody("order-1", "settlement")
	body = strings.Replace(body, `"gross_amount":"100000.00"`, `"gross_amount":"999999.00"`, 1)

	rec := webhookRequest(t, gw, body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "INVALID_SIGNATURE") {
		t.Errorf("body = %s, want INVALID_SIGNATURE", rec.Body.String())
	}

	rec = webhookRequest(t, gw, `{"order_id":"o","transaction_status":"settlement"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no signature status = %d, want 401", rec.Code)
	}
}

func TestWebhookStatusCheckFailure(t *testing.T) {
	gw := stubGateway{err: errors.New("midtrans down")}
	rec := webhookRequest(t, gw, signedWebhookBody("order-1", "settlement"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (Midtrans retries)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "STATUS_CHECK_FAILED") {
		t.Errorf("body = %s, want STATUS_CHECK_FAILED", rec.Body.String())
	}
}

func TestWebhookMalformedBody(t *testing.T) {
	gw := stubGateway{}
	rec := webhookRequest(t, gw, `{`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	rec = webhookRequest(t, gw, `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty body status = %d, want 400", rec.Code)
	}
}

func TestWebhookStatusMismatchStillOK(t *testing.T) {
	// payload says settle, server says pending: still 200, mismatch logged
	// (anomaly, PRD S.7) — the actual trust decision happens at T4/T5.
	gw := stubGateway{status: &payment.Status{TransactionStatus: "pending"}}
	rec := webhookRequest(t, gw, signedWebhookBody("order-1", "settlement"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
