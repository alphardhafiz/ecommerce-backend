package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"ecommerce/server/internal/middleware"
	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
)

type User struct {
	users *repository.UserRepo
}

func NewUser(users *repository.UserRepo) *User {
	return &User{users: users}
}

func (u *User) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	user, err := u.users.FindByID(r.Context(), claims.UserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			respondError(w, http.StatusNotFound, "User not found", "USER_NOT_FOUND", nil)
			return
		}
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    userProfile(user),
	})
}

type updateMeRequest struct {
	Name  string  `json:"name"`
	Phone *string `json:"phone"`
}

func (u *User) UpdateMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	var req updateMeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}
	// Role is never read from the body (PRD S.15) — the struct has no Role field.
	if strings.TrimSpace(req.Name) == "" {
		respondError(w, http.StatusBadRequest, "Validation failed", "VALIDATION_ERROR", []map[string]string{{"field": "name", "message": "Name is required"}})
		return
	}

	user, err := u.users.Update(r.Context(), claims.UserID, strings.TrimSpace(req.Name), req.Phone)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    userProfile(user),
	})
}

func userProfile(u *model.User) map[string]any {
	return map[string]any{
		"id":     u.ID,
		"name":   u.Name,
		"email":  u.Email,
		"role":   u.Role,
		"status": u.Status,
		"phone":  u.Phone,
	}
}
