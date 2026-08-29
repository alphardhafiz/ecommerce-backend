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

type Cart struct {
	svc *service.CartService
}

func NewCart(svc *service.CartService) *Cart {
	return &Cart{svc: svc}
}

func (c *Cart) Get(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	_, items, total, err := c.svc.Get(r.Context(), claims.UserID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	data := make([]map[string]any, 0, len(items))
	for _, item := range items {
		data = append(data, cartItemPayload(item))
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"items": data,
			"total": total,
		},
	})
}

type cartAddItemRequest struct {
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

func (c *Cart) AddItem(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	var req cartAddItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}

	cart, err := c.svc.AddItem(r.Context(), claims.UserID, req.ProductID, req.Quantity)
	if err != nil {
		c.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"data": map[string]any{
			"cart_id": cart.ID,
		},
	})
}

type cartUpdateQtyRequest struct {
	Quantity int `json:"quantity"`
}

func (c *Cart) UpdateItemQty(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	var req cartUpdateQtyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}

	if err := c.svc.UpdateQuantity(r.Context(), claims.UserID, r.PathValue("id"), req.Quantity); err != nil {
		c.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    nil,
	})
}

func (c *Cart) RemoveItem(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	if err := c.svc.RemoveItem(r.Context(), claims.UserID, r.PathValue("id")); err != nil {
		c.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    nil,
	})
}

func (c *Cart) Clear(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	if err := c.svc.Clear(r.Context(), claims.UserID); err != nil {
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    nil,
	})
}

func (c *Cart) respondError(w http.ResponseWriter, err error) {
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
		respondError(w, http.StatusNotFound, "Product or item not found", "NOT_FOUND", nil)
		return
	}
	respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
}

func cartItemPayload(item *model.CartItem) map[string]any {
	return map[string]any{
		"id":            item.ID,
		"product_id":    item.ProductID,
		"name":          item.Name,
		"price":         item.Price,
		"quantity":      item.Quantity,
		"subtotal":      item.Subtotal,
		"is_available":  item.IsAvailable,
		"primary_image": item.PrimaryImage,
	}
}
