package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"

	jwtpkg "ecommerce/server/internal/jwt"
	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
)

const (
	bcryptCost        = 12
	accessTokenTTL    = 15 * time.Minute
	refreshTokenTTL   = 7 * 24 * time.Hour
	refreshTokenBytes = 32
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInactiveAccount    = errors.New("account inactive")
)

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string { return "validation failed" }

type AuthService struct {
	users         *repository.UserRepo
	refreshTokens *repository.RefreshTokenRepo
	jwt           *jwtpkg.Helper
}

func NewAuthService(users *repository.UserRepo, refreshTokens *repository.RefreshTokenRepo, jwt *jwtpkg.Helper) *AuthService {
	return &AuthService{users: users, refreshTokens: refreshTokens, jwt: jwt}
}

type LoginResult struct {
	AccessToken  string
	ExpiresIn    int
	RefreshToken string
	User         *model.User
}

func (s *AuthService) Login(ctx context.Context, email, password string) (*LoginResult, error) {
	user, err := s.users.FindByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	if user.Status != "active" {
		return nil, ErrInactiveAccount
	}

	accessToken, err := s.jwt.Generate(user.ID, user.Role)
	if err != nil {
		return nil, err
	}

	refreshToken, err := randomBytes(refreshTokenBytes)
	if err != nil {
		return nil, err
	}
	rawRefresh := base64.RawURLEncoding.EncodeToString(refreshToken)
	hash := sha256.Sum256(refreshToken)
	if err := s.refreshTokens.Create(ctx, user.ID, hex.EncodeToString(hash[:]), time.Now().Add(refreshTokenTTL)); err != nil {
		return nil, err
	}

	return &LoginResult{
		AccessToken:  accessToken,
		ExpiresIn:    int(accessTokenTTL.Seconds()),
		RefreshToken: rawRefresh,
		User:         user,
	}, nil
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

func (s *AuthService) Register(ctx context.Context, name, email, password, confirmPassword string) (*model.User, error) {
	if verrs := validateRegister(name, email, password, confirmPassword); len(verrs) > 0 {
		return nil, &ValidationError{Errors: verrs}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, err
	}
	return s.users.Create(ctx, strings.TrimSpace(name), strings.ToLower(strings.TrimSpace(email)), string(hash))
}

func validateRegister(name, email, password, confirmPassword string) []FieldError {
	var verrs []FieldError

	if strings.TrimSpace(name) == "" {
		verrs = append(verrs, FieldError{"name", "Name is required"})
	}
	if _, err := mail.ParseAddress(email); err != nil {
		verrs = append(verrs, FieldError{"email", "Email is not valid"})
	}
	if len(password) < 8 || !hasLetterAndDigit(password) {
		verrs = append(verrs, FieldError{"password", "Password must be at least 8 characters with letters and numbers"})
	}
	if password != confirmPassword {
		verrs = append(verrs, FieldError{"confirm_password", "Passwords do not match"})
	}
	return verrs
}

func hasLetterAndDigit(s string) bool {
	hasLetter, hasDigit := false, false
	for _, r := range s {
		if unicode.IsLetter(r) {
			hasLetter = true
		}
		if unicode.IsDigit(r) {
			hasDigit = true
		}
	}
	return hasLetter && hasDigit
}
