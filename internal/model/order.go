package model

import "time"

type Order struct {
	ID              string
	UserID          string
	Status          string
	TotalAmount     int64
	RecipientName   string
	Phone           string
	ShippingAddress string
	ExpiredAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type OrderItem struct {
	ID          string
	OrderID     string
	ProductID   string
	ProductName string
	Price       int64
	Quantity    int
	Subtotal    int64
}
