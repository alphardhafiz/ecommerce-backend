package middleware

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// rateLimiter is the Redis counter surface rate limiting needs (PRD H.3);
// *cache.Cache satisfies it, tests can stub failures.
type rateLimiter interface {
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
}

// RateLimitConfig configures a fixed-window rate limit (PRD H.3).
type RateLimitConfig struct {
	Limit  int
	Window time.Duration
	// FailClosed rejects the request when Redis is down (used for
	// login/register, brute-force surface). General endpoints use
	// FailClosed=false: log + allow (fail-open).
	FailClosed bool
	// Name labels the counter key; empty = r.URL.Path.
	Name string
}

// RateLimit is a fixed-window counter per IP+endpoint
// (ratelimit:{endpoint}:{ip}, PRD H.3). Over the limit -> 429
// TOO_MANY_ATTEMPTS.
func RateLimit(c rateLimiter, cfg RateLimitConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			name := cfg.Name
			if name == "" {
				name = r.URL.Path
			}
			key := "ratelimit:" + name + ":" + clientIP(r)

			n, err := c.Incr(r.Context(), key, cfg.Window)
			if err != nil {
				if cfg.FailClosed {
					slog.Error("rate limit backend down, rejecting", "error", err, "key", key)
					writeError(w, http.StatusServiceUnavailable, "Service unavailable", "RATE_LIMIT_UNAVAILABLE")
					return
				}
				slog.Warn("rate limit backend down, allowing", "error", err, "key", key)
				next.ServeHTTP(w, r)
				return
			}

			if n > int64(cfg.Limit) {
				writeError(w, http.StatusTooManyRequests, "Too many attempts", "TOO_MANY_ATTEMPTS")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
