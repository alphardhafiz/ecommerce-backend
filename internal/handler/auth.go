package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/service"
)

const (
	refreshCookieName   = "refresh_token"
	refreshCookieMaxAge = 7 * 24 * 60 * 60 // 7 days, seconds
)

type Auth struct {
	svc *service.AuthService
}

func NewAuth(svc *service.AuthService) *Auth {
	return &Auth{svc: svc}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (a *Auth) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}

	result, err := a.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			respondError(w, http.StatusUnauthorized, "Invalid email or password", "INVALID_CREDENTIALS", nil)
		case errors.Is(err, service.ErrInactiveAccount):
			respondError(w, http.StatusForbidden, "Account is inactive", "ACCOUNT_INACTIVE", nil)
		default:
			respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		}
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    result.RefreshToken,
		Path:     "/auth",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   refreshCookieMaxAge,
	})

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"access_token": result.AccessToken,
			"expires_in":   result.ExpiresIn,
			"user": map[string]any{
				"id":   result.User.ID,
				"name": result.User.Name,
				"role": result.User.Role,
			},
		},
	})
}

type registerRequest struct {
	Name            string `json:"name"`
	Email           string `json:"email"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
}

func (a *Auth) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}

	user, err := a.svc.Register(r.Context(), req.Name, req.Email, req.Password, req.ConfirmPassword)
	if err != nil {
		var verr *service.ValidationError
		if errors.As(err, &verr) {
			respondError(w, http.StatusBadRequest, "Validation failed", "VALIDATION_ERROR", verr.Errors)
			return
		}
		if errors.Is(err, repository.ErrEmailTaken) {
			respondError(w, http.StatusConflict, "Email already exists", "EMAIL_ALREADY_EXISTS", nil)
			return
		}
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"data": map[string]any{
			"id":    user.ID,
			"name":  user.Name,
			"email": user.Email,
			"role":  user.Role,
		},
	})
}

func respondError(w http.ResponseWriter, status int, message, code string, errorsList any) {
	if errorsList == nil {
		errorsList = []any{}
	}
	respondJSON(w, status, map[string]any{
		"success": false,
		"message": message,
		"code":    code,
		"errors":  errorsList,
	})
}
