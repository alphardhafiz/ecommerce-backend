package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	DatabaseURL string
	RedisURL    string
	JWTSecret   string

	MidtransServerKey    string
	MidtransClientKey    string
	MidtransIsProduction bool

	StorageEndpoint        string
	StorageBucket          string
	StorageAccessKeyID     string
	StorageSecretAccessKey string

	ResendAPIKey    string
	ResendFromEmail string

	FrontendURL       string
	CORSAllowedOrigin string
	Port              string
}

func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:            os.Getenv("DATABASE_URL"),
		RedisURL:               os.Getenv("REDIS_URL"),
		JWTSecret:              os.Getenv("JWT_SECRET"),
		MidtransServerKey:      os.Getenv("MIDTRANS_SERVER_KEY"),
		MidtransClientKey:      os.Getenv("MIDTRANS_CLIENT_KEY"),
		MidtransIsProduction:   os.Getenv("MIDTRANS_IS_PRODUCTION") == "true",
		StorageEndpoint:        os.Getenv("STORAGE_ENDPOINT"),
		StorageBucket:          os.Getenv("STORAGE_BUCKET"),
		StorageAccessKeyID:     os.Getenv("STORAGE_ACCESS_KEY_ID"),
		StorageSecretAccessKey: os.Getenv("STORAGE_SECRET_ACCESS_KEY"),
		ResendAPIKey:           os.Getenv("RESEND_API_KEY"),
		ResendFromEmail:        os.Getenv("RESEND_FROM_EMAIL"),
		FrontendURL:            os.Getenv("FRONTEND_URL"),
		CORSAllowedOrigin:      os.Getenv("CORS_ALLOWED_ORIGIN"),
		Port:                   os.Getenv("PORT"),
	}

	cfg.Port = defaultIfEmpty(cfg.Port, "8080")
	cfg.RedisURL = defaultIfEmpty(cfg.RedisURL, "redis://localhost:6379")
	cfg.FrontendURL = defaultIfEmpty(cfg.FrontendURL, "http://localhost:3000")

	var missing []string
	for name, val := range map[string]string{
		"DATABASE_URL":        cfg.DatabaseURL,
		"JWT_SECRET":          cfg.JWTSecret,
		"CORS_ALLOWED_ORIGIN": cfg.CORSAllowedOrigin,
	} {
		if val == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

func defaultIfEmpty(val, def string) string {
	if val == "" {
		return def
	}
	return val
}
