package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/storage"
)

var ErrStorageFailure = errors.New("object storage failure")

const (
	maxImageSize   = 5 << 20 // 5MB (PRD R.12)
	imageKeyPrefix = "products"
	// productListCacheTTL / productListKeyPrefix per PRD H.1.
	productListCacheTTL  = 5 * time.Minute
	productListKeyPrefix = "products:list:"
	// productDetailCacheTTL / productDetailKeyPrefix per PRD H.2.
	productDetailCacheTTL  = 10 * time.Minute
	productDetailKeyPrefix = "product:detail:"
)

// listCache is the Redis surface the product service needs (PRD H.1/H.2);
// *cache.Cache satisfies it, tests can stub failures.
type listCache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, val []byte, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	InvalidatePrefix(ctx context.Context, prefix string) error
}

type ProductService struct {
	products *repository.ProductRepo
	storage  *storage.Client
	cache    listCache
}

func NewProductService(products *repository.ProductRepo) *ProductService {
	return &ProductService{products: products}
}

// WithStorage attaches the object-storage client for image upload/delete.
func (s *ProductService) WithStorage(st *storage.Client) *ProductService {
	s.storage = st
	return s
}

// WithCache attaches the Redis cache (cache-aside, PRD H.1). Optional:
// without it, or when Redis is down, every call hits the DB.
func (s *ProductService) WithCache(c listCache) *ProductService {
	s.cache = c
	return s
}

type ProductInput struct {
	Name        string
	Description *string
	Price       int64
	Stock       int64
	CategoryID  *string
}

func (s *ProductService) Create(ctx context.Context, in ProductInput) (*model.Product, error) {
	if err := s.validate(in); err != nil {
		return nil, err
	}
	p, err := s.products.Create(ctx, in.Name, in.Description, in.Price, in.Stock, in.CategoryID)
	if err != nil {
		return nil, err
	}
	s.invalidateList(ctx)
	return p, nil
}

func (s *ProductService) Update(ctx context.Context, id string, in ProductInput) (*model.Product, error) {
	if err := s.validate(in); err != nil {
		return nil, err
	}
	p, err := s.products.Update(ctx, id, in.Name, in.Description, in.Price, in.Stock, in.CategoryID)
	if err != nil {
		return nil, err
	}
	s.invalidateList(ctx)
	s.invalidateDetail(ctx, id)
	return p, nil
}

func (s *ProductService) SoftDelete(ctx context.Context, id string) error {
	err := s.products.SoftDelete(ctx, id)
	if err == nil {
		s.invalidateList(ctx)
		s.invalidateDetail(ctx, id)
	}
	return err
}

func (s *ProductService) UpdateStatus(ctx context.Context, id string, isActive bool) (*model.Product, error) {
	p, err := s.products.SetActive(ctx, id, isActive)
	if err == nil {
		s.invalidateList(ctx)
		s.invalidateDetail(ctx, id)
	}
	return p, err
}

func (s *ProductService) UpdateStock(ctx context.Context, id string, stock int) (*model.Product, error) {
	if stock < 0 {
		return nil, &ValidationError{Errors: []FieldError{{Field: "stock", Message: "Stock must be greater than or equal to 0"}}}
	}
	p, err := s.products.SetStock(ctx, id, stock)
	if err == nil {
		s.invalidateList(ctx)
		s.invalidateDetail(ctx, id)
	}
	return p, err
}

type ProductListFilter struct {
	Search     string
	CategoryID string
	MinPrice   int64
	MaxPrice   int64
	HasMin     bool
	HasMax     bool
	InStock    bool
	Sort       string
	Limit      int
	Offset     int
}

// List returns the public product listing, cached under a hash of the
// query filter (PRD H.1: TTL 5m, coarse invalidation on any product
// mutation). Redis failures are logged and ignored — the DB is the source
// of truth (cache-aside, never a hard dependency).
func (s *ProductService) List(ctx context.Context, f ProductListFilter) ([]*model.Product, int64, error) {
	key := productListKey(f)
	if s.cache != nil {
		if raw, err := s.cache.Get(ctx, key); err == nil {
			var cached struct {
				Products []*model.Product `json:"products"`
				Total    int64            `json:"total"`
			}
			if json.Unmarshal(raw, &cached) == nil {
				return cached.Products, cached.Total, nil
			}
			slog.Warn("corrupt product list cache entry", "key", key)
		}
	}

	products, total, err := s.products.ListPublic(ctx, repository.ProductFilter{
		Search:     f.Search,
		CategoryID: f.CategoryID,
		MinPrice:   f.MinPrice,
		MaxPrice:   f.MaxPrice,
		HasMin:     f.HasMin,
		HasMax:     f.HasMax,
		InStock:    f.InStock,
		Sort:       f.Sort,
		Limit:      f.Limit,
		Offset:     f.Offset,
	})
	if err != nil {
		return nil, 0, err
	}

	if s.cache != nil {
		if raw, err := json.Marshal(struct {
			Products []*model.Product `json:"products"`
			Total    int64            `json:"total"`
		}{products, total}); err == nil {
			if err := s.cache.Set(ctx, key, raw, productListCacheTTL); err != nil {
				slog.Warn("product list cache set failed", "error", err)
			}
		}
	}
	return products, total, nil
}

// productListKey hashes the full filter so each unique query has its own
// cache entry (PRD H.1).
func productListKey(f ProductListFilter) string {
	raw := fmt.Sprintf("%s|%s|%d|%d|%t|%t|%t|%s|%d|%d",
		f.Search, f.CategoryID, f.MinPrice, f.MaxPrice, f.HasMin, f.HasMax, f.InStock, f.Sort, f.Limit, f.Offset)
	sum := sha256.Sum256([]byte(raw))
	return productListKeyPrefix + hex.EncodeToString(sum[:])
}

// invalidateList drops every products:list:* key via SCAN+DEL (PRD H.1).
// Coarse on purpose; failures only cost a stale-until-TTL entry.
func (s *ProductService) invalidateList(ctx context.Context) {
	if s.cache == nil {
		return
	}
	if err := s.cache.InvalidatePrefix(ctx, productListKeyPrefix); err != nil {
		slog.Warn("product list cache invalidation failed", "error", err)
	}
}

func productDetailKey(id string) string {
	return productDetailKeyPrefix + id
}

// invalidateDetail drops the single product:detail:{id} key (PRD H.2:
// targeted, no prefix scan).
func (s *ProductService) invalidateDetail(ctx context.Context, id string) {
	if s.cache == nil {
		return
	}
	if err := s.cache.Delete(ctx, productDetailKey(id)); err != nil {
		slog.Warn("product detail cache invalidation failed", "error", err)
	}
}

// GetDetail returns a product, cached per id (PRD H.2: TTL 10m, targeted
// invalidation on product mutations). Redis failures fall back to the DB.
func (s *ProductService) GetDetail(ctx context.Context, id string) (*model.Product, error) {
	key := productDetailKey(id)
	if s.cache != nil {
		if raw, err := s.cache.Get(ctx, key); err == nil {
			p := &model.Product{}
			if json.Unmarshal(raw, p) == nil {
				return p, nil
			}
			slog.Warn("corrupt product detail cache entry", "key", key)
		}
	}

	p, err := s.products.FindPublicByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if s.cache != nil {
		if raw, err := json.Marshal(p); err == nil {
			if err := s.cache.Set(ctx, key, raw, productDetailCacheTTL); err != nil {
				slog.Warn("product detail cache set failed", "error", err)
			}
		}
	}
	return p, nil
}

// UploadImage validates the file (MIME + size, PRD R.12), renames to a UUID,
// stores it, and records the row in product_images. Storage failure is
// reported as ErrStorageFailure so the handler can return 502 (PRD S.12); the
// product itself is untouched.
func (s *ProductService) UploadImage(ctx context.Context, productID string, filename string, data []byte) (*model.ProductImage, error) {
	if s.storage == nil {
		return nil, ErrStorageFailure
	}

	ext, err := validateImage(filename, data)
	if err != nil {
		return nil, err
	}

	key := imageKeyPrefix + "/" + newUUID() + "." + ext
	url, err := s.storage.Upload(ctx, key, data, mimeForExt(ext))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageFailure, err)
	}

	img, err := s.products.CreateImage(ctx, productID, url)
	if err != nil {
		return nil, err
	}
	return img, nil
}

// DeleteImage removes the storage object and its DB row. Returns ErrNotFound
// if the image does not belong to the product.
func (s *ProductService) DeleteImage(ctx context.Context, productID, imageID string) error {
	if s.storage == nil {
		return ErrStorageFailure
	}

	img, err := s.products.FindImage(ctx, productID, imageID)
	if err != nil {
		return err
	}
	if err := s.storage.Delete(ctx, extractKey(img.URL)); err != nil {
		return fmt.Errorf("%w: %v", ErrStorageFailure, err)
	}
	return s.products.DeleteImage(ctx, productID, imageID)
}

var allowedImageExts = map[string]string{
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"png":  "image/png",
	"webp": "image/webp",
}

// validateImage checks size (<=5MB) and MIME (jpg/png/webp), and that the
// declared extension is a single, allowed image extension (no double
// extension like .jpg.php, PRD R.12). Returns the normalized extension.
func validateImage(filename string, data []byte) (string, error) {
	if len(data) > maxImageSize {
		return "", &ValidationError{Errors: []FieldError{{Field: "file", Message: "Image must be at most 5MB"}}}
	}
	if len(data) == 0 {
		return "", &ValidationError{Errors: []FieldError{{Field: "file", Message: "File is empty"}}}
	}

	detected := http.DetectContentType(data)
	if detected != "image/jpeg" && detected != "image/png" && detected != "image/webp" {
		return "", &ValidationError{Errors: []FieldError{{Field: "file", Message: "Only JPG, PNG, or WebP images are allowed"}}}
	}

	ext := strings.ToLower(filepath.Ext(filename))
	ext = strings.TrimPrefix(ext, ".")
	if ext == "jpeg" {
		ext = "jpg"
	}
	if allowedImageExts[ext] != detected {
		return "", &ValidationError{Errors: []FieldError{{Field: "file", Message: "File extension does not match its content"}}}
	}
	return ext, nil
}

func mimeForExt(ext string) string {
	if m, ok := allowedImageExts[ext]; ok {
		return m
	}
	return "application/octet-stream"
}

// extractKey rebuilds the storage key from a public URL (public URLs embed
// the key after the bucket segment).
func extractKey(publicURL string) string {
	// .../storage/v1/object/public/<bucket>/products/<uuid>.<ext>
	idx := strings.LastIndex(publicURL, imageKeyPrefix+"/")
	if idx < 0 {
		return ""
	}
	return publicURL[idx:]
}

// newUUID returns a random UUID v4 string (rename files, PRD R.12).
func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (s *ProductService) validate(in ProductInput) error {
	var errs []FieldError
	if strings.TrimSpace(in.Name) == "" {
		errs = append(errs, FieldError{Field: "name", Message: "Name is required"})
	} else if len(in.Name) > 200 {
		errs = append(errs, FieldError{Field: "name", Message: "Name must be at most 200 characters"})
	}
	if in.Price < 0 {
		errs = append(errs, FieldError{Field: "price", Message: "Price must be greater than or equal to 0"})
	}
	if in.Stock < 0 {
		errs = append(errs, FieldError{Field: "stock", Message: "Stock must be greater than or equal to 0"})
	}
	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}
