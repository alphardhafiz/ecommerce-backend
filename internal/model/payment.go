package model

import "time"

type Payment struct {
	ID              string
	OrderID         string
	MidtransOrderID string
	Status          string
	Amount          int64
	PaymentType     *string
	PaidAt          *time.Time
	SnapToken       *string
	RedirectURL     *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
