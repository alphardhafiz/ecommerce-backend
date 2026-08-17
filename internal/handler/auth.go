package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/service"
)

type Auth struct {
	svc *service.AuthService
}

func NewAuth(svc *service.AuthService) *Auth {
	return &Auth{svc: svc}
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
