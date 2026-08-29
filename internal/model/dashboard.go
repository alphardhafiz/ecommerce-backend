package model

import "time"

// TimeWindow is a half-open [from, to) window over orders.created_at.
type TimeWindow = time.Time

type Dashboard struct {
	TotalUsers     int64
	TotalProducts  int64
	TotalOrders    int64
	OrdersByStatus map[string]int64
	Revenue        int64
	LowStock       []DashboardLowStock
}

type DashboardLowStock struct {
	ID    string
	Name  string
	Stock int
}
