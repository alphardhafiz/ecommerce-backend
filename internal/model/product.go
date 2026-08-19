package model

import "time"

type Product struct {
	ID          string
	CategoryID  *string
	Category    *Category
	Name        string
	Description *string
	Price       int64
	Stock       int
	IsActive    bool
	DeletedAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Images      []ProductImage
}
