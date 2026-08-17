package model

import "time"

type User struct {
	ID           string
	Name         string
	Email        string
	PasswordHash string `json:"-"`
	Role         string
	Status       string
	Phone        *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
