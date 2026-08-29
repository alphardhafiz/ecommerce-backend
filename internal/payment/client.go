package payment

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	serverKey string
	http      *http.Client
	snapURL   string
	apiURL    string
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
