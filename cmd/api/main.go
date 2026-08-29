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
	"ecommerce/server/internal/mail"
	"ecommerce/server/internal/middleware"
	"ecommerce/server/internal/payment"
	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/service"
	"ecommerce/server/internal/storage"
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
	passwordResetTokenRepo := repository.NewPasswordResetTokenRepo(pool)
	jwtHelper := jwtpkg.New(cfg.JWTSecret, jwtpkg.DefaultTTL)
	mailClient := mail.New(cfg.ResendAPIKey, cfg.ResendFromEmail)
	authSvc := service.NewAuthService(userRepo, refreshTokenRepo, passwordResetTokenRepo, jwtHelper, mailClient, cfg.FrontendURL)
	auth := handler.NewAuth(authSvc)
	userSvc := service.NewUserService(userRepo)
	userHandler := handler.NewUser(userSvc)
	categoryRepo := repository.NewCategoryRepo(pool)
	categorySvc := service.NewCategoryService(categoryRepo)
	categoryHandler := handler.NewCategory(categorySvc)
	productRepo := repository.NewProductRepo(pool)
	productSvc := service.NewProductService(productRepo).WithStorage(
		storage.New(cfg.StorageEndpoint, cfg.StorageBucket, cfg.StorageAccessKeyID, cfg.StorageSecretAccessKey))
	productHandler := handler.NewProduct(productSvc)
	wishlistRepo := repository.NewWishlistRepo(pool)
	wishlistSvc := service.NewWishlistService(wishlistRepo)
	wishlistHandler := handler.NewWishlist(wishlistSvc)
	cartRepo := repository.NewCartRepo(pool)
	cartSvc := service.NewCartService(cartRepo, productRepo)
	cartHandler := handler.NewCart(cartSvc)
	addressRepo := repository.NewAddressRepo(pool)
	addressSvc := service.NewAddressService(addressRepo)
	addressHandler := handler.NewAddress(addressSvc)
	orderRepo := repository.NewOrderRepo(pool)
	paymentClient := payment.New(cfg.MidtransServerKey, cfg.MidtransIsProduction)
	if cfg.MidtransServerKey != "" {
		orderRepo.WithGateway(paymentClient)
	}
	orderSvc := service.NewOrderService(orderRepo)
	orderHandler := handler.NewOrder(orderSvc)
	paymentHandler := handler.NewPayment(paymentClient, service.NewPaymentService(repository.NewPaymentRepo(pool)))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health.Liveness)
	mux.HandleFunc("GET /health/ready", health.Readiness)
	mux.HandleFunc("POST /payments/webhook", paymentHandler.Webhook)
	mux.HandleFunc("POST /auth/register", auth.Register)
	mux.HandleFunc("POST /auth/login", auth.Login)
	mux.HandleFunc("POST /auth/refresh", auth.Refresh)
	mux.HandleFunc("POST /auth/logout", auth.Logout)
	mux.HandleFunc("POST /auth/forgot-password", auth.ForgotPassword)
	mux.HandleFunc("POST /auth/reset-password", auth.ResetPassword)
	mux.Handle("GET /users/me", middleware.RequireAuth(jwtHelper)(http.HandlerFunc(userHandler.Me)))
	mux.Handle("PATCH /users/me", middleware.RequireAuth(jwtHelper)(http.HandlerFunc(userHandler.UpdateMe)))
	userRequired := func(next http.Handler) http.Handler {
		return middleware.RequireAuth(jwtHelper)(next)
	}
	mux.Handle("GET /wishlist", userRequired(http.HandlerFunc(wishlistHandler.List)))
	mux.Handle("POST /wishlist", userRequired(http.HandlerFunc(wishlistHandler.Add)))
	mux.Handle("DELETE /wishlist/{productId}", userRequired(http.HandlerFunc(wishlistHandler.Remove)))
	mux.Handle("GET /cart", userRequired(http.HandlerFunc(cartHandler.Get)))
	mux.Handle("POST /cart/items", userRequired(http.HandlerFunc(cartHandler.AddItem)))
	mux.Handle("PATCH /cart/items/{id}", userRequired(http.HandlerFunc(cartHandler.UpdateItemQty)))
	mux.Handle("DELETE /cart/items/{id}", userRequired(http.HandlerFunc(cartHandler.RemoveItem)))
	mux.Handle("DELETE /cart", userRequired(http.HandlerFunc(cartHandler.Clear)))
	mux.Handle("GET /addresses", userRequired(http.HandlerFunc(addressHandler.List)))
	mux.Handle("POST /addresses", userRequired(http.HandlerFunc(addressHandler.Create)))
	mux.Handle("PUT /addresses/{id}", userRequired(http.HandlerFunc(addressHandler.Update)))
	mux.Handle("DELETE /addresses/{id}", userRequired(http.HandlerFunc(addressHandler.Delete)))
	mux.Handle("PATCH /addresses/{id}/default", userRequired(http.HandlerFunc(addressHandler.SetDefault)))
	mux.Handle("POST /orders/checkout", userRequired(http.HandlerFunc(orderHandler.Checkout)))
	mux.Handle("GET /orders", userRequired(http.HandlerFunc(orderHandler.List)))
	mux.Handle("GET /orders/{id}", userRequired(http.HandlerFunc(orderHandler.Get)))
	mux.Handle("POST /orders/{id}/cancel", userRequired(http.HandlerFunc(orderHandler.Cancel)))
	adminRequired := func(next http.Handler) http.Handler {
		return middleware.RequireAuth(jwtHelper)(middleware.RequireRole("admin")(next))
	}
	mux.Handle("GET /admin/orders", adminRequired(http.HandlerFunc(orderHandler.ListAll)))
	mux.Handle("PATCH /admin/orders/{id}/status", adminRequired(http.HandlerFunc(orderHandler.UpdateStatus)))
	mux.Handle("GET /admin/users", adminRequired(http.HandlerFunc(userHandler.ListUsers)))
	mux.Handle("PATCH /admin/users/{id}/status", adminRequired(http.HandlerFunc(userHandler.UpdateUserStatus)))
	mux.HandleFunc("GET /categories", categoryHandler.ListActive)
	mux.HandleFunc("GET /products", productHandler.List)
	mux.HandleFunc("GET /products/{id}", productHandler.Detail)
	mux.Handle("POST /admin/categories", adminRequired(http.HandlerFunc(categoryHandler.Create)))
	mux.Handle("PUT /admin/categories/{id}", adminRequired(http.HandlerFunc(categoryHandler.Update)))
	mux.Handle("DELETE /admin/categories/{id}", adminRequired(http.HandlerFunc(categoryHandler.Delete)))
	mux.Handle("POST /admin/products", adminRequired(http.HandlerFunc(productHandler.Create)))
	mux.Handle("PUT /admin/products/{id}", adminRequired(http.HandlerFunc(productHandler.Update)))
	mux.Handle("DELETE /admin/products/{id}", adminRequired(http.HandlerFunc(productHandler.Delete)))
	mux.Handle("PATCH /admin/products/{id}/status", adminRequired(http.HandlerFunc(productHandler.UpdateStatus)))
	mux.Handle("PATCH /admin/products/{id}/stock", adminRequired(http.HandlerFunc(productHandler.UpdateStock)))
	mux.Handle("POST /admin/products/{id}/images", adminRequired(http.HandlerFunc(productHandler.UploadImage)))
	mux.Handle("DELETE /admin/products/{id}/images/{imageId}", adminRequired(http.HandlerFunc(productHandler.DeleteImage)))

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
