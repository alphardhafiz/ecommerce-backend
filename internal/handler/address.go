package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"ecommerce/server/internal/middleware"
	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/service"
)

type Address struct {
	svc *service.AddressService
}

func NewAddress(svc *service.AddressService) *Address {
	return &Address{svc: svc}
}

type addressCreateRequest struct {
	Label         *string `json:"label"`
	RecipientName string  `json:"recipient_name"`
	Phone         string  `json:"phone"`
	FullAddress   string  `json:"full_address"`
	City          string  `json:"city"`
	Province      string  `json:"province"`
	PostalCode    string  `json:"postal_code"`
	IsDefault     bool    `json:"is_default"`
}

func (a *Address) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	addrs, err := a.svc.List(r.Context(), claims.UserID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	data := make([]map[string]any, 0, len(addrs))
	for _, addr := range addrs {
		data = append(data, addressPayload(addr))
	}

	respondJSON(w, http.StatusOK, map[string]any{"success": true, "data": data})
}

func (a *Address) Create(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	req, err := decodeAddress(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}

	created, err := a.svc.Create(r.Context(), claims.UserID, req)
	if err != nil {
		a.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"data":    addressPayload(created),
	})
}

func (a *Address) Update(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	req, err := decodeAddress(r)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}

	if err := a.svc.Update(r.Context(), claims.UserID, r.PathValue("id"), req); err != nil {
		a.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"success": true, "data": nil})
}

func (a *Address) Delete(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	if err := a.svc.Delete(r.Context(), claims.UserID, r.PathValue("id")); err != nil {
		a.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"success": true, "data": nil})
}

func (a *Address) SetDefault(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	if err := a.svc.SetDefault(r.Context(), claims.UserID, r.PathValue("id")); err != nil {
		a.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"success": true, "data": nil})
}

func (a *Address) respondError(w http.ResponseWriter, err error) {
	var verr *service.ValidationError
	if errors.As(err, &verr) {
		respondError(w, http.StatusBadRequest, "Validation failed", "VALIDATION_ERROR", verr.Errors)
		return
	}
	if errors.Is(err, service.ErrForbidden) {
		respondError(w, http.StatusForbidden, "Forbidden", "FORBIDDEN", nil)
		return
	}
	if errors.Is(err, repository.ErrNotFound) {
		respondError(w, http.StatusNotFound, "Address not found", "NOT_FOUND", nil)
		return
	}
	respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
}

func decodeAddress(r *http.Request) (*model.Address, error) {
	var req addressCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	return &model.Address{
		Label:         req.Label,
		RecipientName: req.RecipientName,
		Phone:         req.Phone,
		FullAddress:   req.FullAddress,
		City:          req.City,
		Province:      req.Province,
		PostalCode:    req.PostalCode,
		IsDefault:     req.IsDefault,
	}, nil
}

func addressPayload(a *model.Address) map[string]any {
	return map[string]any{
		"id":             a.ID,
		"label":          a.Label,
		"recipient_name": a.RecipientName,
		"phone":          a.Phone,
		"full_address":   a.FullAddress,
		"city":           a.City,
		"province":       a.Province,
		"postal_code":    a.PostalCode,
		"is_default":     a.IsDefault,
	}
}
