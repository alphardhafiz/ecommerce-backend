package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
	"ecommerce/server/internal/storage"
)

var ErrStorageFailure = errors.New("object storage failure")

const (
	maxImageSize   = 5 << 20 // 5MB (PRD R.12)
	imageKeyPrefix = "products"
)

type ProductService struct {
	products *repository.ProductRepo
	storage  *storage.Client
}

func NewProductService(products *repository.ProductRepo) *ProductService {
	return &ProductService{products: products}
}

// WithStorage attaches the object-storage client for image upload/delete.
func (s *ProductService) WithStorage(st *storage.Client) *ProductService {
	s.storage = st
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
	return s.products.Create(ctx, in.Name, in.Description, in.Price, in.Stock, in.CategoryID)
}

func (s *ProductService) Update(ctx context.Context, id string, in ProductInput) (*model.Product, error) {
	if err := s.validate(in); err != nil {
		return nil, err
	}
	return s.products.Update(ctx, id, in.Name, in.Description, in.Price, in.Stock, in.CategoryID)
}

func (s *ProductService) SoftDelete(ctx context.Context, id string) error {
	return s.products.SoftDelete(ctx, id)
}

func (s *ProductService) UpdateStatus(ctx context.Context, id string, isActive bool) (*model.Product, error) {
	return s.products.SetActive(ctx, id, isActive)
}

func (s *ProductService) UpdateStock(ctx context.Context, id string, stock int) (*model.Product, error) {
	if stock < 0 {
		return nil, &ValidationError{Errors: []FieldError{{Field: "stock", Message: "Stock must be greater than or equal to 0"}}}
	}
	return s.products.SetStock(ctx, id, stock)
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

func (s *ProductService) List(ctx context.Context, f ProductListFilter) ([]*model.Product, int64, error) {
	return s.products.ListPublic(ctx, repository.ProductFilter{
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
}

func (s *ProductService) GetDetail(ctx context.Context, id string) (*model.Product, error) {
	return s.products.FindPublicByID(ctx, id)
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

	order := 0
	img, err := s.products.CreateImage(ctx, productID, url, order)
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
