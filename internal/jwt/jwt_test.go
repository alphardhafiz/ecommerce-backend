package jwt

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestGenerateValidateRoundTrip(t *testing.T) {
	h := New("secret", DefaultTTL)
	token, err := h.Generate("user-1", "admin")
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	claims, err := h.Validate(token)
	if err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	if claims.UserID != "user-1" || claims.Role != "admin" {
		t.Errorf("claims = %+v, want user-1/admin", claims)
	}
	if claims.JTI == "" {
		t.Error("JTI is empty, want random jti")
	}
	ttl := claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time)
	if ttl > DefaultTTL || ttl < DefaultTTL-time.Minute {
		t.Errorf("ttl = %v, want ~15m", ttl)
	}
}

func TestValidateExpired(t *testing.T) {
	h := New("secret", -time.Minute)
	token, err := h.Generate("user-1", "user")
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	_, err = h.Validate(token)
	if !errors.Is(err, ErrExpired) {
		t.Errorf("Validate() error = %v, want ErrExpired", err)
	}
}

func TestValidateWrongSecret(t *testing.T) {
	token, err := New("secret-a", DefaultTTL).Generate("user-1", "user")
	if err != nil {
		t.Fatal(err)
	}
	_, err = New("secret-b", DefaultTTL).Validate(token)
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("Validate() error = %v, want ErrInvalid", err)
	}
}

func TestValidateMissingClaims(t *testing.T) {
	claims := jwt.MapClaims{
		"sub": "user-1",
		"exp": jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	ss, err := token.SignedString([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = New("secret", DefaultTTL).Validate(ss)
	if !errors.Is(err, ErrMissingClaim) {
		t.Errorf("Validate() error = %v, want ErrMissingClaim", err)
	}
}

func TestValidateAlgNone(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"user_id": "user-1",
		"role":    "user",
		"jti":     "x",
	})
	ss, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	_, err = New("secret", DefaultTTL).Validate(ss)
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("Validate() error = %v, want ErrInvalid (alg=none rejected)", err)
	}
}
