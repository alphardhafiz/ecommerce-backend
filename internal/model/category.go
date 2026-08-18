package model

import "time"

type Category struct {
	ID        string
	Name      string
	Slug      string
	IsActive  bool
	DeletedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}
