package payment

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	sandboxSnapBase = "https://app.sandbox.midtrans.com"
	prodSnapBase    = "https://app.midtrans.com"
	sandboxAPIBase  = "https://api.sandbox.midtrans.com"
	prodAPIBase     = "https://api.midtrans.com"
)

// Client wraps the Midtrans Snap / Core APIs. Money is passed as int64 whole
// rupiah (PRD D: no floats).
type Client struct {
	serverKey       string
	http            *http.Client
	snapURL         string
	apiURL          string
	notificationURL string
	frontendURL     string
}

func New(serverKey string, isProduction bool) *Client {
	snapBase, apiBase := sandboxSnapBase, sandboxAPIBase
	if isProduction {
		snapBase, apiBase = prodSnapBase, prodAPIBase
	}
	return &Client{
		serverKey: serverKey,
		http:      &http.Client{Timeout: 15 * time.Second},
		snapURL:   snapBase,
		apiURL:    apiBase,
	}
}

// WithNotificationURL sets the webhook Midtrans POSTs to; when empty,
// Midtrans falls back to the dashboard setting.
func (c *Client) WithNotificationURL(u string) *Client {
	c.notificationURL = u
	return c
}

// WithFrontendURL sets the base for the finish/unfinish/error callbacks
// (PRD F.1: redirect is UX-only, never a status source).
func (c *Client) WithFrontendURL(u string) *Client {
	c.frontendURL = u
	return c
}

// TransactionParams is the minimal data Midtrans needs to create a Snap
// transaction (PRD F.1: order_id + amount).
type TransactionParams struct {
	OrderID     string
	GrossAmount int64
}

type Transaction struct {
	Token       string `json:"token"`
	RedirectURL string `json:"redirect_url"`
}

func (c *Client) CreateTransaction(ctx context.Context, p TransactionParams) (*Transaction, error) {
	payload := map[string]any{
		"transaction_details": map[string]any{
			"order_id":     p.OrderID,
			"gross_amount": p.GrossAmount,
		},
	}
	if c.notificationURL != "" {
		payload["notification_url"] = c.notificationURL
	}
	if c.frontendURL != "" {
		payload["callbacks"] = map[string]string{
			"finish":   c.frontendURL + "/payment/finish",
			"unfinish": c.frontendURL + "/payment/unfinish",
			"error":    c.frontendURL + "/payment/error",
		}
	}
	var out Transaction
	err := c.doJSON(ctx, http.MethodPost, c.snapURL+"/snap/v1/transactions", payload, &out)
	if err != nil {
		return nil, fmt.Errorf("midtrans create transaction: %w", err)
	}
	return &out, nil
}

// Status is the server-to-server double-check payload (PRD F.2).
type Status struct {
	TransactionStatus string `json:"transaction_status"`
	FraudStatus       string `json:"fraud_status"`
	StatusCode        string `json:"status_code"`
	GrossAmount       string `json:"gross_amount"`
	PaymentType       string `json:"payment_type"`
}

func (c *Client) GetStatus(ctx context.Context, orderID string) (*Status, error) {
	var out Status
	err := c.doJSON(ctx, http.MethodGet, c.apiURL+"/v2/"+orderID+"/status", nil, &out)
	if err != nil {
		return nil, fmt.Errorf("midtrans status check: %w", err)
	}
	return &out, nil
}

// Notification is the payload Midtrans POSTs to /payments/webhook (PRD
// F.2). Financial fields stay strings: signature verification concatenates
// them exactly as Midtrans sent them.
type Notification struct {
	OrderID           string `json:"order_id"`
	StatusCode        string `json:"status_code"`
	GrossAmount       string `json:"gross_amount"`
	SignatureKey      string `json:"signature_key"`
	TransactionStatus string `json:"transaction_status"`
	PaymentType       string `json:"payment_type"`
}

// VerifySignature checks that the notification's signature_key equals
// SHA-512(order_id + status_code + gross_amount + server_key) (PRD C.10).
// Only a holder of the server key can forge this.
func (c *Client) VerifySignature(n Notification) bool {
	sum := sha512.Sum512([]byte(n.OrderID + n.StatusCode + n.GrossAmount + c.serverKey))
	return strings.EqualFold(hex.EncodeToString(sum[:]), n.SignatureKey)
}

func (c *Client) doJSON(ctx context.Context, method, url string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.serverKey+":")))

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return midtransError(resp.StatusCode, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode midtrans response: %w", err)
		}
	}
	return nil
}

// midtransError surfaces Midtrans' status_message when present.
func midtransError(code int, raw []byte) error {
	var msg struct {
		StatusMessage string `json:"status_message"`
	}
	if json.Unmarshal(raw, &msg) == nil && msg.StatusMessage != "" {
		return fmt.Errorf("midtrans status %d: %s", code, msg.StatusMessage)
	}
	return fmt.Errorf("midtrans status %d", code)
}
