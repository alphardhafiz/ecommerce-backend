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

type Wishlist struct {
	svc *service.WishlistService
}

func NewWishlist(svc *service.WishlistService) *Wishlist {
	return &Wishlist{svc: svc}
}

type wishlistAddRequest struct {
	ProductID string `json:"product_id"`
}

func (w *Wishlist) Add(wr http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(wr, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	var req wishlistAddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(wr, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}
	if req.ProductID == "" {
		respondError(wr, http.StatusBadRequest, "Validation failed", "VALIDATION_ERROR",
			[]map[string]string{{"field": "product_id", "message": "product_id is required"}})
		return
	}

	if err := w.svc.Add(r.Context(), claims.UserID, req.ProductID); err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			respondError(wr, http.StatusNotFound, "Product not found", "PRODUCT_NOT_FOUND", nil)
		case errors.Is(err, repository.ErrAlreadyInWishlist):
			respondError(wr, http.StatusConflict, "Product already in wishlist", "PRODUCT_ALREADY_IN_WISHLIST", nil)
		default:
			respondError(wr, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		}
		return
	}

	respondJSON(wr, http.StatusCreated, map[string]any{
		"success": true,
		"data":    map[string]any{"product_id": req.ProductID},
	})
}

func (w *Wishlist) List(wr http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(wr, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	items, err := w.svc.List(r.Context(), claims.UserID)
	if err != nil {
		respondError(wr, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	data := make([]map[string]any, 0, len(items))
	for _, item := range items {
		data = append(data, wishlistPayload(item))
	}

	respondJSON(wr, http.StatusOK, map[string]any{
		"success": true,
		"data":    data,
	})
}

func (w *Wishlist) Remove(wr http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(wr, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	if err := w.svc.Remove(r.Context(), claims.UserID, r.PathValue("productId")); err != nil {
		respondError(wr, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	respondJSON(wr, http.StatusOK, map[string]any{
		"success": true,
		"data":    nil,
	})
}

func wishlistPayload(item *model.WishlistItem) map[string]any {
	return map[string]any{
		"product_id":    item.ProductID,
		"product_name":  item.ProductName,
		"price":         item.Price,
		"stock":         item.Stock,
		"in_stock":      item.InStock,
		"is_active":     item.IsActive,
		"primary_image": item.PrimaryImage,
		"added_at":      item.AddedAt,
	}
}
