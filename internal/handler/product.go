package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/service"
)

type Product struct {
	svc *service.ProductService
}

func NewProduct(svc *service.ProductService) *Product {
	return &Product{svc: svc}
}

type productRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Price       int64   `json:"price"`
	Stock       int64   `json:"stock"`
	CategoryID  *string `json:"category_id"`
}

func (p *Product) Create(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeProductRequest(w, r)
	if !ok {
		return
	}

	product, err := p.svc.Create(r.Context(), req)
	if err != nil {
		p.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"data":    productPayload(product),
	})
}

func (p *Product) Update(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeProductRequest(w, r)
	if !ok {
		return
	}

	product, err := p.svc.Update(r.Context(), r.PathValue("id"), req)
	if err != nil {
		p.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    productPayload(product),
	})
}

func (p *Product) Delete(w http.ResponseWriter, r *http.Request) {
	if err := p.svc.SoftDelete(r.Context(), r.PathValue("id")); err != nil {
		p.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    nil,
	})
}

func decodeProductRequest(w http.ResponseWriter, r *http.Request) (service.ProductInput, bool) {
	var req productRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return service.ProductInput{}, false
	}
	return service.ProductInput{
		Name:        req.Name,
		Description: req.Description,
		Price:       req.Price,
		Stock:       req.Stock,
		CategoryID:  req.CategoryID,
	}, true
}

func (p *Product) respondError(w http.ResponseWriter, err error) {
	var verr *service.ValidationError
	if errors.As(err, &verr) {
		respondError(w, http.StatusBadRequest, "Validation failed", "VALIDATION_ERROR", verr.Errors)
		return
	}
	if errors.Is(err, repository.ErrNotFound) {
		respondError(w, http.StatusNotFound, "Product not found", "PRODUCT_NOT_FOUND", nil)
		return
	}
	if errors.Is(err, repository.ErrCategoryNotFound) {
		respondError(w, http.StatusNotFound, "Category not found", "CATEGORY_NOT_FOUND", nil)
		return
	}
	respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
}

func productPayload(p *model.Product) map[string]any {
	return map[string]any{
		"id":          p.ID,
		"category_id": p.CategoryID,
		"name":        p.Name,
		"description": p.Description,
		"price":       p.Price,
		"stock":       p.Stock,
		"is_active":   p.IsActive,
	}
}
