package mail

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendPasswordReset(t *testing.T) {
	var gotFrom, gotTo, gotSubject, gotHTML, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		gotFrom, _ = body["from"].(string)
		gotTo, _ = body["to"].([]any)[0].(string)
		gotSubject, _ = body["subject"].(string)
		gotHTML, _ = body["html"].(string)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := New("test-key", "noreply@example.com")
	c.http = srv.Client()
	c.resendURL = srv.URL

	if err := c.SendPasswordReset("budi@example.com", "http://localhost:3000/reset-password?token=abc"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", gotAuth)
	}
	if gotFrom != "noreply@example.com" || gotTo != "budi@example.com" || gotSubject == "" {
		t.Errorf("payload = from=%q to=%q subject=%q", gotFrom, gotTo, gotSubject)
	}
	if !strings.Contains(gotHTML, "token=abc") {
		t.Errorf("html = %q, want reset link", gotHTML)
	}
}

func TestSendPasswordResetNoAPIKey(t *testing.T) {
	c := New("", "noreply@example.com")
	if err := c.SendPasswordReset("budi@example.com", "http://localhost:3000/reset-password?token=abc"); err != nil {
		t.Fatalf("dev mode (no key) should not error, got %v", err)
	}
}

func TestSendPasswordResetAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := New("test-key", "noreply@example.com")
	c.http = srv.Client()
	c.resendURL = srv.URL

	if err := c.SendPasswordReset("budi@example.com", "http://localhost:3000/reset-password?token=abc"); err == nil {
		t.Fatal("want error on non-2xx, got nil")
	}
}
