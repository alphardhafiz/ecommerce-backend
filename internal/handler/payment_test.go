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
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"ecommerce/server/internal/payment"
	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/service"
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

func signedWebhookBody(orderID, midtransStatus string) string {
	n := payment.Notification{
		OrderID:           orderID,
		StatusCode:        "200",
		GrossAmount:       "100000.00",
		TransactionStatus: midtransStatus,
	}
	sum := sha512.Sum512([]byte(n.OrderID + n.StatusCode + n.GrossAmount + webhookServerKey))
	n.SignatureKey = hex.EncodeToString(sum[:])
	b, _ := json.Marshal(n)
	return string(b)
}

// webhookPaymentHandler builds the real handler with the real DB so
// idempotency can be asserted against payments/payment_notifications.
func webhookPaymentHandler(t *testing.T, gw PaymentGateway) (*Payment, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping webhook test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return NewPayment(gw, service.NewPaymentService(repository.NewPaymentRepo(pool))), pool
}

// seedPayment inserts an order + payments row (PENDING) directly, returning
// the order id; the checkout flow already inserts payments (T2), this
// shortcut keeps webhook tests focused.
func seedPayment(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()
	seedUser(t, pool, email, "abc12345", "active")
	var userID string
	pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)

	var orderID string
	pool.QueryRow(context.Background(),
		`INSERT INTO orders (user_id, status, total_amount, recipient_name, phone, shipping_address)
		 VALUES ($1, 'PENDING', 100000, 'Budi', '08123456789', 'Jl. Merdeka No. 1')
		 RETURNING id`, userID).Scan(&orderID)
	pool.Exec(context.Background(),
		`INSERT INTO payments (order_id, midtrans_order_id, status, amount)
		 VALUES ($1, $2::text, 'PENDING', 100000)`, orderID, orderID)
	return orderID
}

func webhookRequest(t *testing.T, h *Payment, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/payments/webhook", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Webhook(rec, req)
	return rec
}

// cleanupWebhookOrder removes notification audit rows first: payment_notifications
// references payments, payments references orders (no cascades anywhere).
func cleanupWebhookOrder(pool *pgxpool.Pool, orderID, email string) {
	pool.Exec(context.Background(),
		`DELETE FROM payment_notifications WHERE payment_id IN (SELECT id FROM payments WHERE order_id = $1)`, orderID)
	pool.Exec(context.Background(), `DELETE FROM payments WHERE order_id = $1`, orderID)
	pool.Exec(context.Background(), `DELETE FROM orders WHERE id = $1`, orderID)
	pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)
}

func TestWebhookValidSignature(t *testing.T) {
	h, pool := webhookPaymentHandler(t, stubGateway{status: &payment.Status{TransactionStatus: "settlement"}})
	orderID := seedPayment(t, pool, "wh-valid@example.com")
	defer cleanupWebhookOrder(pool, orderID, "wh-valid@example.com")

	rec := webhookRequest(t, h, signedWebhookBody(orderID, "settlement"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}

	var payStatus string
	pool.QueryRow(context.Background(), `SELECT status FROM payments WHERE order_id = $1`, orderID).Scan(&payStatus)
	if payStatus != "SUCCESS" {
		t.Errorf("payment status = %s, want SUCCESS", payStatus)
	}
	var n int
	pool.QueryRow(context.Background(),
		`SELECT count(*) FROM payment_notifications pn JOIN payments p ON p.id = pn.payment_id WHERE p.order_id = $1`, orderID).Scan(&n)
	if n != 1 {
		t.Errorf("notifications = %d, want 1", n)
	}
}

func TestWebhookInvalidSignature(t *testing.T) {
	h, pool := webhookPaymentHandler(t, stubGateway{status: &payment.Status{TransactionStatus: "settlement"}})
	orderID := seedPayment(t, pool, "wh-invalid@example.com")
	defer cleanupWebhookOrder(pool, orderID, "wh-invalid@example.com")

	body := signedWebhookBody(orderID, "settlement")
	body = strings.Replace(body, `"gross_amount":"100000.00"`, `"gross_amount":"999999.00"`, 1)

	rec := webhookRequest(t, h, body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "INVALID_SIGNATURE") {
		t.Errorf("body = %s, want INVALID_SIGNATURE", rec.Body.String())
	}

	var n int
	pool.QueryRow(context.Background(),
		`SELECT count(*) FROM payment_notifications pn JOIN payments p ON p.id = pn.payment_id WHERE p.order_id = $1`, orderID).Scan(&n)
	if n != 0 {
		t.Errorf("notifications = %d, want 0 (invalid delivery not logged)", n)
	}
}

func TestWebhookStatusCheckFailure(t *testing.T) {
	h, pool := webhookPaymentHandler(t, stubGateway{err: errors.New("midtrans down")})
	orderID := seedPayment(t, pool, "wh-checkfail@example.com")
	defer cleanupWebhookOrder(pool, orderID, "wh-checkfail@example.com")

	rec := webhookRequest(t, h, signedWebhookBody(orderID, "settlement"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (Midtrans retries)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "STATUS_CHECK_FAILED") {
		t.Errorf("body = %s, want STATUS_CHECK_FAILED", rec.Body.String())
	}
}

func TestWebhookMalformedBody(t *testing.T) {
	h, pool := webhookPaymentHandler(t, stubGateway{})
	defer pool.Close()
	rec := webhookRequest(t, h, `{`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	rec = webhookRequest(t, h, `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty body status = %d, want 400", rec.Code)
	}
}

// TestWebhookDuplicateDelivery: Midtrans retries the same settlement
// notification; both calls return 200, the payment stays SUCCESS, and every
// delivery is still logged (audit trail, PRD C.10).
func TestWebhookDuplicateDelivery(t *testing.T) {
	h, pool := webhookPaymentHandler(t, stubGateway{status: &payment.Status{TransactionStatus: "settlement"}})
	orderID := seedPayment(t, pool, "wh-dup@example.com")
	defer cleanupWebhookOrder(pool, orderID, "wh-dup@example.com")

	body := signedWebhookBody(orderID, "settlement")
	for i := 0; i < 2; i++ {
		rec := webhookRequest(t, h, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("delivery %d status = %d", i, rec.Code)
		}
	}

	var payStatus string
	pool.QueryRow(context.Background(), `SELECT status FROM payments WHERE order_id = $1`, orderID).Scan(&payStatus)
	if payStatus != "SUCCESS" {
		t.Errorf("payment status = %s, want SUCCESS", payStatus)
	}
	var n int
	pool.QueryRow(context.Background(),
		`SELECT count(*) FROM payment_notifications pn JOIN payments p ON p.id = pn.payment_id WHERE p.order_id = $1`, orderID).Scan(&n)
	if n != 2 {
		t.Errorf("notifications = %d, want 2 (every delivery logged)", n)
	}
}

func TestWebhookExpireMapsToExpired(t *testing.T) {
	h, pool := webhookPaymentHandler(t, stubGateway{status: &payment.Status{TransactionStatus: "expire"}})
	orderID := seedPayment(t, pool, "wh-expire@example.com")
	defer cleanupWebhookOrder(pool, orderID, "wh-expire@example.com")

	rec := webhookRequest(t, h, signedWebhookBody(orderID, "expire"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var payStatus string
	pool.QueryRow(context.Background(), `SELECT status FROM payments WHERE order_id = $1`, orderID).Scan(&payStatus)
	if payStatus != "EXPIRED" {
		t.Errorf("payment status = %s, want EXPIRED", payStatus)
	}
}

func TestWebhookUnknownOrderStillOK(t *testing.T) {
	h, pool := webhookPaymentHandler(t, stubGateway{status: &payment.Status{TransactionStatus: "settlement"}})
	defer pool.Close()

	rec := webhookRequest(t, h, signedWebhookBody("00000000-0000-0000-0000-000000000000", "settlement"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (stop Midtrans retries, PRD S.7)", rec.Code)
	}
}
