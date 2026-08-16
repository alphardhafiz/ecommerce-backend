package config

import (
	"os"
	"testing"
)

func TestLoadRequiresMandatoryVars(t *testing.T) {
	os.Clearenv()
	_, err := Load()
	if err == nil {
		t.Fatal("expected error when required env vars are missing")
	}
}

func TestLoadDefaults(t *testing.T) {
	os.Clearenv()
	t.Setenv("DATABASE_URL", "postgres://localhost/db")
	t.Setenv("JWT_SECRET", "secret")
	t.Setenv("CORS_ALLOWED_ORIGIN", "http://localhost:3000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port default = %q, want 8080", cfg.Port)
	}
	if cfg.RedisURL != "redis://localhost:6379" {
		t.Errorf("RedisURL default = %q", cfg.RedisURL)
	}
	if cfg.MidtransIsProduction {
		t.Error("MidtransIsProduction default = true, want false")
	}
}
