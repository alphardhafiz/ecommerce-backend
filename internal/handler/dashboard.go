package handler

import (
	"errors"
	"net/http"

	"ecommerce/server/internal/service"
)

type Dashboard struct {
	svc *service.DashboardService
}

func NewDashboard(svc *service.DashboardService) *Dashboard {
	return &Dashboard{svc: svc}
}

// Get returns admin dashboard metrics (PRD C.11) with an optional
// period/custom-range filter.
func (d *Dashboard) Get(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	metrics, err := d.svc.Get(r.Context(), q.Get("period"), q.Get("start_date"), q.Get("end_date"))
	if err != nil {
		var verr *service.ValidationError
		if errors.As(err, &verr) {
			respondError(w, http.StatusBadRequest, "Validation failed", "VALIDATION_ERROR", verr.Errors)
			return
		}
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	lowStock := make([]map[string]any, 0, len(metrics.LowStock))
	for _, p := range metrics.LowStock {
		lowStock = append(lowStock, map[string]any{
			"id":    p.ID,
			"name":  p.Name,
			"stock": p.Stock,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"total_users":      metrics.TotalUsers,
			"total_products":   metrics.TotalProducts,
			"total_orders":     metrics.TotalOrders,
			"orders_by_status": metrics.OrdersByStatus,
			"revenue":          metrics.Revenue,
			"low_stock":        lowStock,
		},
	})
}
