package service

import (
	"context"
	"strings"
	"unicode"

	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
)

type CategoryService struct {
	categories *repository.CategoryRepo
}

func NewCategoryService(categories *repository.CategoryRepo) *CategoryService {
	return &CategoryService{categories: categories}
}

func (s *CategoryService) ListActive(ctx context.Context) ([]*model.Category, error) {
	return s.categories.ListActive(ctx)
}

func (s *CategoryService) Create(ctx context.Context, name string) (*model.Category, error) {
	if strings.TrimSpace(name) == "" {
		return nil, &ValidationError{Errors: []FieldError{{Field: "name", Message: "Name is required"}}}
	}
	return s.categories.Create(ctx, strings.TrimSpace(name), slugify(name))
}

func (s *CategoryService) Update(ctx context.Context, id, name string) (*model.Category, error) {
	if strings.TrimSpace(name) == "" {
		return nil, &ValidationError{Errors: []FieldError{{Field: "name", Message: "Name is required"}}}
	}
	return s.categories.Update(ctx, id, strings.TrimSpace(name), slugify(name))
}

func (s *CategoryService) SoftDelete(ctx context.Context, id string) error {
	return s.categories.SoftDelete(ctx, id)
}

// slugify builds a URL slug from a display name (lowercase, a-z/0-9 joined
// by dashes). Non-ASCII letters are dropped; enough for Indonesian category
// names and matches the UNIQUE(slug) constraint.
func slugify(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_' || unicode.IsSpace(r):
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
