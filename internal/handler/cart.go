package handler

import (
	"net/http"

	"ecommerce/server/internal/middleware"
	"ecommerce/server/internal/model"
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

func cartItemPayload(item *model.CartItem) map[string]any {
	return map[string]any{
		"id":           item.ID,
		"product_id":   item.ProductID,
		"name":         item.Name,
		"price":        item.Price,
		"quantity":     item.Quantity,
		"subtotal":     item.Subtotal,
		"is_available": item.IsAvailable,
	}
}
