package service

import (
	"context"
	"net/mail"
	"strings"
	"unicode"

	"golang.org/x/crypto/bcrypt"

	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
)

const bcryptCost = 12

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string { return "validation failed" }

type AuthService struct {
	users *repository.UserRepo
}

func NewAuthService(users *repository.UserRepo) *AuthService {
	return &AuthService{users: users}
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
