package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestIDGeneratesWhenMissing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if GetRequestID(r.Context()) == "" {
			t.Error("request id not set in context")
		}
	}))
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID header not set in response")
	}
}

func TestRequestIDAdoptsIncomingHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "custom-id")
	rec := httptest.NewRecorder()

	handler := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := GetRequestID(r.Context()); got != "custom-id" {
			t.Errorf("context id = %q, want custom-id", got)
		}
	}))
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-ID"); got != "custom-id" {
		t.Errorf("response header id = %q, want custom-id", got)
	}
}
