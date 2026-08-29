package payment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateTransaction(t *testing.T) {
	var gotMethod, gotAuth, gotPath string
	var gotPayload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotAuth, gotPath = r.Method, r.Header.Get("Authorization"), r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotPayload)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"token":"snap-tok-123","redirect_url":"https://app.sandbox.midtrans.com/snap/v3/redirection/snap-tok-123"}`))
	}))
	defer srv.Close()

	c := &Client{serverKey: "SB-Mid-server-test", http: srv.Client(), snapURL: srv.URL}
	tx, err := c.CreateTransaction(context.Background(), TransactionParams{OrderID: "order-1", GrossAmount: 50000})
	if err != nil {
		t.Fatal(err)
	}
	if tx.Token != "snap-tok-123" || !strings.Contains(tx.RedirectURL, "snap-tok-123") {
		t.Errorf("tx = %+v", tx)
	}
	if gotMethod != http.MethodPost || gotPath != "/snap/v1/transactions" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if gotAuth != "Basic U0ItTWlkLXNlcnZlci10ZXN0Og==" {
		t.Errorf("auth = %s", gotAuth)
	}
	details, ok := gotPayload["transaction_details"].(map[string]any)
	if !ok || details["order_id"] != "order-1" || details["gross_amount"] != float64(50000) {
		t.Errorf("payload = %v", gotPayload)
	}
}

func TestCreateTransactionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"status_code":"400","status_message":"gross_amount should be numeric"}`))
	}))
	defer srv.Close()

	c := &Client{serverKey: "k", http: srv.Client(), snapURL: srv.URL}
	_, err := c.CreateTransaction(context.Background(), TransactionParams{OrderID: "o", GrossAmount: 1})
	if err == nil || !strings.Contains(err.Error(), "gross_amount should be numeric") {
		t.Errorf("err = %v", err)
	}
}

func TestGetStatus(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"transaction_status":"settlement","fraud_status":"accept","status_code":"200","gross_amount":"50000.00","payment_type":"bank_transfer"}`))
	}))
	defer srv.Close()

	c := &Client{serverKey: "SB-Mid-server-test", http: srv.Client(), apiURL: srv.URL}
	st, err := c.GetStatus(context.Background(), "order-1")
	if err != nil {
		t.Fatal(err)
	}
	if st.TransactionStatus != "settlement" || st.FraudStatus != "accept" || st.PaymentType != "bank_transfer" {
		t.Errorf("st = %+v", st)
	}
	if gotPath != "/v2/order-1/status" || !strings.HasPrefix(gotAuth, "Basic ") {
		t.Errorf("path=%s auth=%s", gotPath, gotAuth)
	}
}

func TestBaseURLSelection(t *testing.T) {
	sandbox := New("k", false)
	prod := New("k", true)
	if sandbox.snapURL != sandboxSnapBase || sandbox.apiURL != sandboxAPIBase {
		t.Errorf("sandbox urls = %s %s", sandbox.snapURL, sandbox.apiURL)
	}
	if prod.snapURL != prodSnapBase || prod.apiURL != prodAPIBase {
		t.Errorf("prod urls = %s %s", prod.snapURL, prod.apiURL)
	}
}
