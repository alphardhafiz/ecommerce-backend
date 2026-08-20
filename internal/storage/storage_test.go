package storage

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestSignAuthorizationHeaderShape(t *testing.T) {
	c := New("https://example.com/storage/v1/s3", "bucket", "AKID", "SECRET")
	req, err := http.NewRequest(http.MethodPut, c.objectURL("products/abc.png"), strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	c.sign(req, sha256Hex([]byte("x")))

	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AKID/") {
		t.Errorf("Authorization = %q, want AWS4-HMAC-SHA256 Credential prefix", auth)
	}
	if !strings.Contains(auth, "SignedHeaders=host;x-amz-content-sha256;x-amz-date") {
		t.Errorf("Authorization = %q, want signed headers list", auth)
	}
	if !strings.Contains(auth, "Signature=") {
		t.Errorf("Authorization = %q, want signature", auth)
	}
	if req.Header.Get("x-amz-date") == "" || req.Header.Get("x-amz-content-sha256") == "" {
		t.Error("x-amz-date and x-amz-content-sha256 headers must be set")
	}
}

func TestDevModeUploadNoCredentials(t *testing.T) {
	c := New("", "", "", "")
	url, err := c.Upload(context.Background(), "products/x.png", []byte("data"), "image/png")
	if err != nil {
		t.Fatalf("dev-mode upload must not fail: %v", err)
	}
	if !strings.Contains(url, "products/x.png") {
		t.Errorf("dev-mode URL = %q, want it to contain the key", url)
	}
	if err := c.Delete(context.Background(), "products/x.png"); err != nil {
		t.Errorf("dev-mode delete must be a no-op: %v", err)
	}
}
