package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"ecommerce/server/internal/middleware"
	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/service"
)

type Order struct {
	svc *service.OrderService
}

func NewOrder(svc *service.OrderService) *Order {
	return &Order{svc: svc}
}

type orderCheckoutRequest struct {
	CartItemIDs []string `json:"cart_item_ids"`
	AddressID   string   `json:"address_id"`
}

func (o *Order) Checkout(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	var req orderCheckoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}

	order, err := o.svc.Checkout(r.Context(), claims.UserID, req.CartItemIDs, req.AddressID)
	if err != nil {
		o.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"data": map[string]any{
			"order_id":     order.ID,
			"total_amount": order.TotalAmount,
			"status":       order.Status,
			"payment":      nil, // payment stub: Midtrans integration lands in Fase 6
		},
	})
}

func (o *Order) respondError(w http.ResponseWriter, err error) {
	var verr *service.ValidationError
	if errors.As(err, &verr) {
		respondError(w, http.StatusBadRequest, "Validation failed", "VALIDATION_ERROR", verr.Errors)
		return
	}
	var perr *repository.ProductError
	if errors.As(err, &perr) {
		respondError(w, http.StatusConflict, perr.ProductName+" is "+perr.Code, perr.Code,
			[]map[string]string{{"field": "product_id", "message": perr.ProductID}})
		return
	}
	switch {
	case errors.Is(err, repository.ErrCartEmpty):
		respondError(w, http.StatusBadRequest, "Cart is empty", "CART_EMPTY", nil)
	case errors.Is(err, repository.ErrInvalidAddress):
		respondError(w, http.StatusBadRequest, "Address is invalid", "INVALID_ADDRESS", nil)
	case errors.Is(err, repository.ErrProductNotFound):
		respondError(w, http.StatusNotFound, "Product not found", "PRODUCT_NOT_FOUND", nil)
	default:
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
	}
}
