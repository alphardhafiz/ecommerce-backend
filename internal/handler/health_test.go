package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"ecommerce/server/internal/cache"
)

func TestLiveness(t *testing.T) {
	h := NewHealth(nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.Liveness(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || body.Data.Status != "ok" {
		t.Errorf("body = %+v, want success=true status=ok", body)
	}
}

func TestReadinessDBDownRedisDown(t *testing.T) {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, "postgres://localhost:1/x")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()

	deadRedis, err := cache.New("redis://localhost:16380")
	if err != nil {
		t.Fatal(err)
	}
	defer deadRedis.Close()

	h := NewHealth(pool, deadRedis)
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()

	h.Readiness(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when DB down", rec.Code)
	}
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			DB    string `json:"db"`
			Redis string `json:"redis"`
		} `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Success {
		t.Error("success = true, want false when DB down")
	}
	if body.Data.DB != "down" {
		t.Errorf("db = %q, want down", body.Data.DB)
	}
}

// Requires local DB + Redis (docker compose). Skipped when env not set.
func TestReadinessAllUp(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" || os.Getenv("REDIS_URL") == "" {
		t.Skip("DATABASE_URL and REDIS_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	redisCache, err := cache.New(os.Getenv("REDIS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer redisCache.Close()

	h := NewHealth(pool, redisCache)
	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	rec := httptest.NewRecorder()

	h.Readiness(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", rec.Code, rec.Body.String())
	}
}
