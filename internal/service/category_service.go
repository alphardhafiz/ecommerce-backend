package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
)

// categoriesCacheKey / categoriesCacheTTL per PRD H.4.
const (
	categoriesCacheKey = "categories:active"
	categoriesCacheTTL = 30 * time.Minute
)

type CategoryService struct {
	categories *repository.CategoryRepo
	cache      listCache
}

func NewCategoryService(categories *repository.CategoryRepo) *CategoryService {
	return &CategoryService{categories: categories}
}

// WithCache attaches the Redis cache (PRD H.4); optional, Redis down =
// direct DB (fail-open, PRD H).
func (s *CategoryService) WithCache(c listCache) *CategoryService {
	s.cache = c
	return s
}

// ListActive returns the active categories, cached under a single key (PRD
// H.4: TTL 30m, invalidated on any category CRUD).
func (s *CategoryService) ListActive(ctx context.Context) ([]*model.Category, error) {
	if s.cache != nil {
		if raw, err := s.cache.Get(ctx, categoriesCacheKey); err == nil {
			var cats []*model.Category
			if json.Unmarshal(raw, &cats) == nil {
				return cats, nil
			}
			slog.Warn("corrupt category cache entry", "key", categoriesCacheKey)
		}
	}

	cats, err := s.categories.ListActive(ctx)
	if err != nil {
		return nil, err
	}

	if s.cache != nil {
		if raw, err := json.Marshal(cats); err == nil {
			if err := s.cache.Set(ctx, categoriesCacheKey, raw, categoriesCacheTTL); err != nil {
				slog.Warn("category cache set failed", "error", err)
			}
		}
	}
	return cats, nil
}

func (s *CategoryService) Create(ctx context.Context, name string) (*model.Category, error) {
	if strings.TrimSpace(name) == "" {
		return nil, &ValidationError{Errors: []FieldError{{Field: "name", Message: "Name is required"}}}
	}
	c, err := s.categories.Create(ctx, strings.TrimSpace(name), slugify(name))
	if err == nil {
		s.invalidate(ctx)
	}
	return c, err
}

func (s *CategoryService) Update(ctx context.Context, id, name string) (*model.Category, error) {
	if strings.TrimSpace(name) == "" {
		return nil, &ValidationError{Errors: []FieldError{{Field: "name", Message: "Name is required"}}}
	}
	c, err := s.categories.Update(ctx, id, strings.TrimSpace(name), slugify(name))
	if err == nil {
		s.invalidate(ctx)
	}
	return c, err
}

func (s *CategoryService) SoftDelete(ctx context.Context, id string) error {
	err := s.categories.SoftDelete(ctx, id)
	if err == nil {
		s.invalidate(ctx)
	}
	return err
}

func (s *CategoryService) invalidate(ctx context.Context) {
	if s.cache == nil {
		return
	}
	if err := s.cache.Delete(ctx, categoriesCacheKey); err != nil {
		slog.Warn("category cache invalidation failed", "error", err)
	}
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
