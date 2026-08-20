package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

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

func (p *Product) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page := parsePositiveInt(q.Get("page"), 1)
	limit := parsePositiveInt(q.Get("limit"), 12)
	if limit > 50 {
		limit = 50
	}

	filter := service.ProductListFilter{
		Search:     q.Get("search"),
		CategoryID: q.Get("category_id"),
		Sort:       q.Get("sort"),
		Limit:      limit,
		Offset:     (page - 1) * limit,
	}

	switch filter.Sort {
	case "", "newest", "price_asc", "price_desc", "name_asc":
	default:
		respondError(w, http.StatusBadRequest, "Invalid query parameter", "INVALID_QUERY_PARAM", nil)
		return
	}

	if v := q.Get("min_price"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			respondError(w, http.StatusBadRequest, "Invalid query parameter", "INVALID_QUERY_PARAM", nil)
			return
		}
		filter.MinPrice, filter.HasMin = n, true
	}
	if v := q.Get("max_price"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			respondError(w, http.StatusBadRequest, "Invalid query parameter", "INVALID_QUERY_PARAM", nil)
			return
		}
		filter.MaxPrice, filter.HasMax = n, true
	}
	if v := q.Get("in_stock"); v != "" {
		switch v {
		case "true":
			filter.InStock = true
		case "false":
		default:
			respondError(w, http.StatusBadRequest, "Invalid query parameter", "INVALID_QUERY_PARAM", nil)
			return
		}
	}

	products, total, err := p.svc.List(r.Context(), filter)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	data := make([]map[string]any, 0, len(products))
	for _, prod := range products {
		data = append(data, productPublicPayload(prod))
	}
	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(limit) - 1) / int64(limit))
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    data,
		"meta": map[string]any{
			"page":        page,
			"limit":       limit,
			"total":       total,
			"total_pages": totalPages,
		},
	})
}

func (p *Product) Detail(w http.ResponseWriter, r *http.Request) {
	product, err := p.svc.GetDetail(r.Context(), r.PathValue("id"))
	if err != nil {
		p.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    productDetailPayload(product),
	})
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

type productStatusRequest struct {
	IsActive bool `json:"is_active"`
}

func (p *Product) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	var req productStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}

	product, err := p.svc.UpdateStatus(r.Context(), r.PathValue("id"), req.IsActive)
	if err != nil {
		p.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    productPayload(product),
	})
}

type productStockRequest struct {
	Stock int `json:"stock"`
}

func (p *Product) UpdateStock(w http.ResponseWriter, r *http.Request) {
	var req productStockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST", nil)
		return
	}

	product, err := p.svc.UpdateStock(r.Context(), r.PathValue("id"), req.Stock)
	if err != nil {
		p.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    productPayload(product),
	})
}

const maxUploadBytes = 5 << 20 // 5MB file limit (PRD R.12)

// maxMultipartBody allows the 5MB file plus multipart overhead (boundary,
// per-part headers) to pass MaxBytesReader. The authoritative file-size check
// stays in the service (validateImage on the decoded bytes).
const maxMultipartBody = maxUploadBytes + (1 << 20)

func (p *Product) UploadImage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMultipartBody)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		respondError(w, http.StatusBadRequest, "File too large or invalid multipart form", "VALIDATION_ERROR",
			[]map[string]string{{"field": "file", "message": "Image must be at most 5MB"}})
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		respondError(w, http.StatusBadRequest, "Missing image file", "VALIDATION_ERROR",
			[]map[string]string{{"field": "file", "message": "image field is required"}})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR", nil)
		return
	}

	img, err := p.svc.UploadImage(r.Context(), r.PathValue("id"), header.Filename, data)
	if err != nil {
		p.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"data":    productImagePayload(img),
	})
}

func (p *Product) DeleteImage(w http.ResponseWriter, r *http.Request) {
	if err := p.svc.DeleteImage(r.Context(), r.PathValue("id"), r.PathValue("imageId")); err != nil {
		p.respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data":    nil,
	})
}

func productImagePayload(img *model.ProductImage) map[string]any {
	return map[string]any{
		"id":         img.ID,
		"url":        img.URL,
		"is_primary": img.IsPrimary,
		"order":      img.DisplayOrder,
	}
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
	if errors.Is(err, repository.ErrImageNotFound) {
		respondError(w, http.StatusNotFound, "Image not found", "IMAGE_NOT_FOUND", nil)
		return
	}
	if errors.Is(err, service.ErrStorageFailure) {
		respondError(w, http.StatusBadGateway, "Storage upload failed", "STORAGE_UPLOAD_FAILED", nil)
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

// productPublicPayload is the public listing shape (PRD E): includes category
// object and primary_image. Images arrive in T7 — null until then.
func productPublicPayload(p *model.Product) map[string]any {
	var category map[string]any
	if p.Category != nil && p.Category.ID != "" {
		category = map[string]any{
			"id":   p.Category.ID,
			"name": p.Category.Name,
		}
	}
	return map[string]any{
		"id":            p.ID,
		"name":          p.Name,
		"price":         p.Price,
		"stock":         p.Stock,
		"is_active":     p.IsActive,
		"primary_image": nil,
		"category":      category,
	}
}

// productDetailPayload is the public detail shape (PRD C.3): full product with
// category and all images.
func productDetailPayload(p *model.Product) map[string]any {
	var category map[string]any
	if p.Category != nil && p.Category.ID != "" {
		category = map[string]any{
			"id":   p.Category.ID,
			"name": p.Category.Name,
		}
	}
	images := make([]map[string]any, 0, len(p.Images))
	for _, img := range p.Images {
		images = append(images, map[string]any{
			"id":         img.ID,
			"url":        img.URL,
			"is_primary": img.IsPrimary,
			"order":      img.DisplayOrder,
		})
	}
	return map[string]any{
		"id":          p.ID,
		"name":        p.Name,
		"description": p.Description,
		"price":       p.Price,
		"stock":       p.Stock,
		"in_stock":    p.Stock > 0,
		"is_active":   p.IsActive,
		"category":    category,
		"images":      images,
	}
}
