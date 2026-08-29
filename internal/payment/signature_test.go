package payment

import (
	"crypto/sha512"
	"encoding/hex"
	"testing"
)

func TestVerifySignature(t *testing.T) {
	c := New("SB-Mid-server-test-key", false)
	n := Notification{
		OrderID:           "order-123",
		StatusCode:        "200",
		GrossAmount:       "100000.00",
		TransactionStatus: "settlement",
	}
	sum := sha512.Sum512([]byte(n.OrderID + n.StatusCode + n.GrossAmount + c.serverKey))
	n.SignatureKey = hex.EncodeToString(sum[:])

	if !c.VerifySignature(n) {
		t.Error("valid signature rejected")
	}

	tampered := n
	tampered.GrossAmount = "200000.00"
	if c.VerifySignature(tampered) {
		t.Error("tampered gross_amount accepted")
	}

	wrongKey := n
	wrongKey.OrderID = "other-order"
	if c.VerifySignature(wrongKey) {
		t.Error("tampered order_id accepted")
	}

	if c.VerifySignature(Notification{OrderID: "x", StatusCode: "200", GrossAmount: "1", SignatureKey: ""}) {
		t.Error("empty signature accepted")
	}
}
