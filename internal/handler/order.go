package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"ecommerce/server/internal/middleware"
	"ecommerce/server/internal/model"
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

func (o *Order) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	q := r.URL.Query()
	page := parsePositiveInt(q.Get("page"), 1)
	limit := parsePositiveInt(q.Get("limit"), 12)
	if limit > 50 {
		limit = 50
	}

	orders, itemsByOrder, total, err := o.svc.List(r.Context(), claims.UserID, limit, (page-1)*limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	data := make([]map[string]any, 0, len(orders))
	for _, order := range orders {
		data = append(data, orderPayload(order, itemsByOrder[order.ID]))
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

func (o *Order) Get(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	order, items, err := o.svc.Get(r.Context(), claims.UserID, r.PathValue("id"))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrForbidden):
			respondError(w, http.StatusForbidden, "Forbidden", "FORBIDDEN", nil)
		case errors.Is(err, repository.ErrNotFound):
			respondError(w, http.StatusNotFound, "Order not found", "NOT_FOUND", nil)
		default:
			respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		}
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    orderPayload(order, items),
	})
}

func orderPayload(order *model.Order, items []*model.OrderItem) map[string]any {
	itemData := make([]map[string]any, 0, len(items))
	for _, it := range items {
		itemData = append(itemData, map[string]any{
			"id":           it.ID,
			"product_id":   it.ProductID,
			"product_name": it.ProductName,
			"price":        it.Price,
			"quantity":     it.Quantity,
			"subtotal":     it.Subtotal,
		})
	}
	return map[string]any{
		"id":               order.ID,
		"status":           order.Status,
		"total_amount":     order.TotalAmount,
		"recipient_name":   order.RecipientName,
		"phone":            order.Phone,
		"shipping_address": order.ShippingAddress,
		"expired_at":       order.ExpiredAt,
		"created_at":       order.CreatedAt,
		"items":            itemData,
	}
}

func (o *Order) Cancel(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED", nil)
		return
	}

	if err := o.svc.Cancel(r.Context(), claims.UserID, r.PathValue("id")); err != nil {
		switch {
		case errors.Is(err, service.ErrForbidden):
			respondError(w, http.StatusForbidden, "Forbidden", "FORBIDDEN", nil)
		case errors.Is(err, repository.ErrNotFound):
			respondError(w, http.StatusNotFound, "Order not found", "NOT_FOUND", nil)
		case errors.Is(err, repository.ErrOrderNotCancellable):
			respondError(w, http.StatusConflict, "Order cannot be cancelled", "ORDER_CANNOT_BE_CANCELLED", nil)
		default:
			respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		}
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"success": true, "data": nil})
}

var orderStatuses = map[string]bool{
	"PENDING": true, "PAID": true, "PROCESSING": true, "SHIPPED": true,
	"COMPLETED": true, "CANCELLED": true, "EXPIRED": true,
}

func (o *Order) ListAll(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := parsePositiveInt(q.Get("page"), 1)
	limit := parsePositiveInt(q.Get("limit"), 12)
	if limit > 50 {
		limit = 50
	}

	status := strings.ToUpper(strings.TrimSpace(q.Get("status")))
	if status != "" && !orderStatuses[status] {
		respondError(w, http.StatusBadRequest, "Invalid query parameter", "INVALID_QUERY_PARAM", nil)
		return
	}

	orders, itemsByOrder, total, err := o.svc.ListAll(r.Context(), status, limit, (page-1)*limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	data := make([]map[string]any, 0, len(orders))
	for _, order := range orders {
		data = append(data, orderPayload(order, itemsByOrder[order.ID]))
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

type orderUpdateStatusRequest struct {
	Status string `json:"status"`
}

func (o *Order) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	var req orderUpdateStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}
	req.Status = strings.ToUpper(strings.TrimSpace(req.Status))
	if !orderStatuses[req.Status] {
		respondError(w, http.StatusBadRequest, "Validation failed", "VALIDATION_ERROR",
			[]map[string]string{{"field": "status", "message": "status must be one of PENDING/PAID/PROCESSING/SHIPPED/COMPLETED/CANCELLED/EXPIRED"}})
		return
	}

	if err := o.svc.UpdateStatus(r.Context(), r.PathValue("id"), req.Status); err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			respondError(w, http.StatusNotFound, "Order not found", "NOT_FOUND", nil)
		case errors.Is(err, repository.ErrInvalidStatusTransition):
			respondError(w, http.StatusConflict, "Invalid status transition", "INVALID_STATUS_TRANSITION", nil)
		default:
			respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		}
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"success": true, "data": nil})
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
