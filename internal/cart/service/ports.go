package service

import (
	"context"

	"goshop/internal/cart/model"
)

//go:generate mockery --name=CartRepository
type CartRepository interface {
	GetOrCreateCart(ctx context.Context, userID string) (*model.Cart, error)
	AddProduct(ctx context.Context, userID, productID string, quantity uint) (*model.Cart, error)
	RemoveProduct(ctx context.Context, userID, productID string) (*model.Cart, error)
}
