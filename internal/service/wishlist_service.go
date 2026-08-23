package service

import (
	"context"

	"ecommerce/server/internal/model"
	"ecommerce/server/internal/repository"
)

type WishlistService struct {
	wishlist *repository.WishlistRepo
}

func NewWishlistService(wishlist *repository.WishlistRepo) *WishlistService {
	return &WishlistService{wishlist: wishlist}
}

func (s *WishlistService) Add(ctx context.Context, userID, productID string) error {
	return s.wishlist.Add(ctx, userID, productID)
}

func (s *WishlistService) List(ctx context.Context, userID string) ([]*model.WishlistItem, error) {
	return s.wishlist.List(ctx, userID)
}

func (s *WishlistService) Remove(ctx context.Context, userID, productID string) error {
	return s.wishlist.Remove(ctx, userID, productID)
}
