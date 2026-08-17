package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"ecommerce/server/internal/middleware"
	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/service"
)

type User struct {
	svc *service.UserService
}

func NewUser(svc *service.UserService) *User {
	return &User{svc: svc}
}

func (u *User) Me(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	user, err := u.svc.Me(r.Context(), claims.UserID)
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

	user, err := u.svc.UpdateMe(r.Context(), claims.UserID, req.Name, req.Phone)
	if err != nil {
		var verr *service.ValidationError
		if errors.As(err, &verr) {
			respondError(w, http.StatusBadRequest, "Validation failed", "VALIDATION_ERROR", verr.Errors)
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

func (u *User) ListUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page := parsePositiveInt(q.Get("page"), 1)
	limit := parsePositiveInt(q.Get("limit"), 12)
	if limit > 50 {
		limit = 50
	}

	status := strings.TrimSpace(q.Get("status"))
	if status != "" && status != "active" && status != "inactive" {
		respondError(w, http.StatusBadRequest, "Invalid query parameter", "INVALID_QUERY_PARAM", nil)
		return
	}

	users, total, err := u.svc.List(r.Context(), status, limit, (page-1)*limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	data := make([]map[string]any, 0, len(users))
	for _, usr := range users {
		data = append(data, userProfile(usr))
	}
	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(limit) - 1) / int64(limit))
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    data,
		"meta": map[string]any{
			"page":        page,
			"limit":       limit,
			"total":       total,
			"total_pages": totalPages,
		},
	})
}

func parsePositiveInt(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

type updateUserStatusRequest struct {
	Status string `json:"status"`
}

func (u *User) UpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	var req updateUserStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}

	user, err := u.svc.UpdateStatus(r.Context(), r.PathValue("id"), strings.TrimSpace(req.Status))
	if err != nil {
		var verr *service.ValidationError
		if errors.As(err, &verr) {
			respondError(w, http.StatusBadRequest, "Validation failed", "VALIDATION_ERROR", verr.Errors)
			return
		}
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
