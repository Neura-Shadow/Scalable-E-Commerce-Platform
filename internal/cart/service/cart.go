package service

import (
	"context"

	"github.com/quangdangfit/gocommon/validation"

	"goshop/internal/cart/dto"
	"goshop/internal/cart/model"
)

//go:generate mockery --name=ICartService
type ICartService interface {
	AddProduct(ctx context.Context, req *dto.AddProductReq) (*model.Cart, error)
	GetCartByUserID(ctx context.Context, userID string) (*model.Cart, error)
	RemoveProduct(ctx context.Context, req *dto.RemoveProductReq) (*model.Cart, error)
}

type CartService struct {
	validator validation.Validation
	repo      CartRepository
}

func NewCartService(
	validator validation.Validation,
	repo CartRepository,
) *CartService {
	return &CartService{
		validator: validator,
		repo:      repo,
	}
}

func (p *CartService) GetCartByUserID(ctx context.Context, userID string) (*model.Cart, error) {
	return p.repo.GetOrCreateCart(ctx, userID)
}

func (p *CartService) AddProduct(ctx context.Context, req *dto.AddProductReq) (*model.Cart, error) {
	if err := p.validator.ValidateStruct(req); err != nil {
		return nil, err
	}

	return p.repo.AddProduct(ctx, req.UserID, req.Line.ProductID, req.Line.Quantity)
}

func (p *CartService) RemoveProduct(ctx context.Context, req *dto.RemoveProductReq) (*model.Cart, error) {
	if err := p.validator.ValidateStruct(req); err != nil {
		return nil, err
	}

	return p.repo.RemoveProduct(ctx, req.UserID, req.ProductID)
}
