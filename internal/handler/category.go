package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/service"
)

type Category struct {
	svc *service.CategoryService
}

func NewCategory(svc *service.CategoryService) *Category {
	return &Category{svc: svc}
}

func (c *Category) ListActive(w http.ResponseWriter, r *http.Request) {
	categories, err := c.svc.ListActive(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	data := make([]map[string]any, 0, len(categories))
	for _, cat := range categories {
		data = append(data, categoryPayload(cat))
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    data,
	})
}

type categoryRequest struct {
	Name string `json:"name"`
}

func (c *Category) Create(w http.ResponseWriter, r *http.Request) {
	var req categoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}

	cat, err := c.svc.Create(r.Context(), req.Name)
	if err != nil {
		c.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"data":    categoryPayload(cat),
	})
}

func (c *Category) Update(w http.ResponseWriter, r *http.Request) {
	var req categoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}

	cat, err := c.svc.Update(r.Context(), r.PathValue("id"), req.Name)
	if err != nil {
		c.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    categoryPayload(cat),
	})
}

func (c *Category) Delete(w http.ResponseWriter, r *http.Request) {
	if err := c.svc.SoftDelete(r.Context(), r.PathValue("id")); err != nil {
		c.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    nil,
	})
}

func (c *Category) respondError(w http.ResponseWriter, err error) {
	var verr *service.ValidationError
	if errors.As(err, &verr) {
		respondError(w, http.StatusBadRequest, "Validation failed", "VALIDATION_ERROR", verr.Errors)
		return
	}
	if errors.Is(err, repository.ErrNotFound) {
		respondError(w, http.StatusNotFound, "Category not found", "CATEGORY_NOT_FOUND", nil)
		return
	}
	if errors.Is(err, repository.ErrSlugTaken) {
		respondError(w, http.StatusConflict, "Category with this slug already exists", "CATEGORY_SLUG_EXISTS", nil)
		return
	}
	respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
}

func categoryPayload(c *model.Category) map[string]any {
	return map[string]any{
		"id":        c.ID,
		"name":      c.Name,
		"slug":      c.Slug,
		"is_active": c.IsActive,
	}
}
