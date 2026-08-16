package jwt

import (
	"crypto/rand"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrInvalid      = errors.New("invalid token")
	ErrExpired      = errors.New("token expired")
	ErrMissingClaim = errors.New("token missing required claim")
)

const DefaultTTL = 15 * time.Minute

type Claims struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
	JTI    string `json:"jti"`
	jwt.RegisteredClaims
}

type Helper struct {
	secret []byte
	ttl    time.Duration
}

func New(secret string, ttl time.Duration) *Helper {
	return &Helper{secret: []byte(secret), ttl: ttl}
}

func (h *Helper) Generate(userID, role string) (string, error) {
	jti, err := randomHex(16)
	if err != nil {
		return "", err
	}
	now := time.Now()
	claims := Claims{
		UserID: userID,
		Role:   role,
		JTI:    jti,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(h.ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(h.secret)
}

func (h *Helper) Validate(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalid
		}
		return h.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpired
		}
		return nil, ErrInvalid
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalid
	}
	if claims.UserID == "" || claims.Role == "" || claims.JTI == "" {
		return nil, ErrMissingClaim
	}
	return claims, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	const hex = "0123456789abcdef"
	out := make([]byte, n*2)
	for i, v := range b {
		out[i*2] = hex[v>>4]
		out[i*2+1] = hex[v&0x0f]
	}
	return string(out), nil
}
