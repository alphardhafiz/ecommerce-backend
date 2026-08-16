package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"ecommerce/server/internal/cache"
)

type Health struct {
	pool  *pgxpool.Pool
	cache *cache.Cache
}

func NewHealth(pool *pgxpool.Pool, cache *cache.Cache) *Health {
	return &Health{pool: pool, cache: cache}
}

// Liveness: process is alive, always 200.
func (h *Health) Liveness(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    map[string]string{"status": "ok"},
	})
}

// Readiness: DB ping decides 200/503. Redis is optional: down Redis logs a
// warning in the response but keeps 200 (PRD H, N.2).
func (h *Health) Readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	var dbCheck int
	dbErr := h.pool.QueryRow(ctx, "SELECT 1").Scan(&dbCheck)
	redisErr := h.cache.Ping(ctx)

	data := map[string]any{"db": "up", "redis": "up"}
	status := http.StatusOK
	if dbErr != nil {
		status = http.StatusServiceUnavailable
		data["db"] = "down"
	}
	if redisErr != nil {
		data["redis"] = "down (optional, app still serving)"
	}

	respondJSON(w, status, map[string]any{
		"success": status == http.StatusOK,
		"data":    data,
	})
}

func respondJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
