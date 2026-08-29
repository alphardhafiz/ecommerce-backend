package service

import (
	"context"
	"time"

	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
)

type DashboardService struct {
	dashboard *repository.DashboardRepo
}

func NewDashboardService(dashboard *repository.DashboardRepo) *DashboardService {
	return &DashboardService{dashboard: dashboard}
}

// Get returns dashboard metrics. period is one of today/7d/30d (or empty =
// all time); a custom range is expressed via startDate/endDate in
// YYYY-MM-DD (PRD C.11). startDate alone means "from that date on",
// endDate alone means "up to that date".
func (s *DashboardService) Get(ctx context.Context, period, startDate, endDate string) (*model.Dashboard, error) {
	var from, to *model.TimeWindow

	switch period {
	case "":
	case "today":
		start := time.Now().Truncate(24 * time.Hour)
		from, to = &start, nil
	case "7d", "30d":
		days := 7
		if period == "30d" {
			days = 30
		}
		start := time.Now().AddDate(0, 0, -days)
		from, to = &start, nil
	default:
		return nil, &ValidationError{Errors: []FieldError{{Field: "period", Message: "period must be one of today/7d/30d"}}}
	}

	if startDate != "" {
		t, err := time.Parse("2006-01-02", startDate)
		if err != nil {
			return nil, &ValidationError{Errors: []FieldError{{Field: "start_date", Message: "start_date must be YYYY-MM-DD"}}}
		}
		from = &t
	}
	if endDate != "" {
		t, err := time.Parse("2006-01-02", endDate)
		if err != nil {
			return nil, &ValidationError{Errors: []FieldError{{Field: "end_date", Message: "end_date must be YYYY-MM-DD"}}}
		}
		// half-open window: include the whole endDate day
		end := t.Add(24 * time.Hour)
		to = &end
	}

	return s.dashboard.Get(ctx, from, to)
}
