package service

import (
	"context"
	"strings"

	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
)

type UserService struct {
	users *repository.UserRepo
}

func NewUserService(users *repository.UserRepo) *UserService {
	return &UserService{users: users}
}

func (s *UserService) Me(ctx context.Context, userID string) (*model.User, error) {
	return s.users.FindByID(ctx, userID)
}

func (s *UserService) UpdateMe(ctx context.Context, userID, name string, phone *string) (*model.User, error) {
	if strings.TrimSpace(name) == "" {
		return nil, &ValidationError{Errors: []FieldError{{Field: "name", Message: "Name is required"}}}
	}
	return s.users.Update(ctx, userID, strings.TrimSpace(name), phone)
}

func (s *UserService) List(ctx context.Context, status string, limit, offset int) ([]*model.User, int64, error) {
	return s.users.List(ctx, status, limit, offset)
}

func (s *UserService) UpdateStatus(ctx context.Context, userID, status string) (*model.User, error) {
	if status != "active" && status != "inactive" {
		return nil, &ValidationError{Errors: []FieldError{{Field: "status", Message: "Status must be active or inactive"}}}
	}
	return s.users.UpdateStatus(ctx, userID, status)
}
