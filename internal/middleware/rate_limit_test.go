package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"ecommerce/server/internal/cache"
)

func newRateLimitCache(t *testing.T) *cache.Cache {
	t.Helper()
	url := os.Getenv("REDIS_URL")
	if url == "" {
		url = "redis://localhost:16379"
	}
	c, err := cache.New(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Ping(ctx); err != nil {
		t.Skipf("redis not reachable, skipping: %v", err)
	}
	return c
}

func TestRateLimitExceeded(t *testing.T) {
	c := newRateLimitCache(t)
	ctx := context.Background()
	defer c.Delete(ctx, "ratelimit:login:192.0.2.1", "ratelimit:login:192.0.2.2")

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RateLimit(c, RateLimitConfig{Limit: 2, Window: time.Minute, Name: "login"})(ok)

	for i := 1; i <= 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		req.RemoteAddr = "192.0.2.1:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200", i, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request 3 = %d, want 429", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "TOO_MANY_ATTEMPTS") {
		t.Errorf("body = %s, want TOO_MANY_ATTEMPTS", rec.Body.String())
	}

	// different IP is not limited
	req2 := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req2.RemoteAddr = "192.0.2.2:1234"
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("other IP = %d, want 200", rec2.Code)
	}
}

// deadRateLimiter is a cache whose Redis is unreachable.
type deadRateLimiter struct{}

func (deadRateLimiter) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	return 0, os.ErrNotExist
}

func TestRateLimitFailClosedOnRedisDown(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RateLimit(deadRateLimiter{}, RateLimitConfig{Limit: 5, Window: time.Minute, FailClosed: true, Name: "login"})(ok)

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("fail-closed = %d, want 503", rec.Code)
	}
}

func TestRateLimitFailOpenOnRedisDown(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RateLimit(deadRateLimiter{}, RateLimitConfig{Limit: 100, Window: time.Minute})(ok)

	req := httptest.NewRequest(http.MethodGet, "/products", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("fail-open = %d, want 200", rec.Code)
	}
}
