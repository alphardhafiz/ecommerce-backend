package model

import "time"

type ProductImage struct {
	ID           string
	URL          string
	IsPrimary    bool
	DisplayOrder int
	CreatedAt    time.Time
}
