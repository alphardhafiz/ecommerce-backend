package service

import (
	"context"
	"strings"

	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
)

type ProductService struct {
	products *repository.ProductRepo
}

func NewProductService(products *repository.ProductRepo) *ProductService {
	return &ProductService{products: products}
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
