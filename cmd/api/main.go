package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"

	"ecommerce/server/internal/cache"
	"ecommerce/server/internal/config"
	"ecommerce/server/internal/database"
	"ecommerce/server/internal/handler"
	jwtpkg "ecommerce/server/internal/jwt"
	"ecommerce/server/internal/middleware"
	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/service"
	"ecommerce/server/pkg/logger"
)

func main() {
	log := logger.New()

	// Dev convenience: load .env if present; ignore missing file (prod uses real env).
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		log.Error("config load failed", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("database pool creation failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	redisCache, err := cache.New(cfg.RedisURL)
	if err != nil {
		log.Error("redis client init failed", "error", err)
		os.Exit(1)
	}
	defer redisCache.Close()

	health := handler.NewHealth(pool, redisCache)

	userRepo := repository.NewUserRepo(pool)
	refreshTokenRepo := repository.NewRefreshTokenRepo(pool)
	jwtHelper := jwtpkg.New(cfg.JWTSecret, jwtpkg.DefaultTTL)
	authSvc := service.NewAuthService(userRepo, refreshTokenRepo, jwtHelper)
	auth := handler.NewAuth(authSvc)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health.Liveness)
	mux.HandleFunc("GET /health/ready", health.Readiness)
	mux.HandleFunc("POST /auth/register", auth.Register)
	mux.HandleFunc("POST /auth/login", auth.Login)
	mux.HandleFunc("POST /auth/refresh", auth.Refresh)
	mux.HandleFunc("POST /auth/logout", auth.Logout)

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: middleware.RequestID()(middleware.Logging(log)(mux)),
	}

	log.Info("server starting", "port", cfg.Port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}
