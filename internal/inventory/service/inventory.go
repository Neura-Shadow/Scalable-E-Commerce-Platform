package service

import (
	"context"
	"errors"

	"github.com/quangdangfit/gocommon/validation"
	"gorm.io/gorm"

	"goshop/internal/inventory/dto"
	"goshop/internal/inventory/model"
	"goshop/internal/inventory/repository"
	"goshop/pkg/paging"
)

var (
	ErrInventoryNotFound = errors.New("inventory not found")
	ErrInsufficientStock = errors.New("insufficient stock")
)

//go:generate mockery --name=IInventoryService
type IInventoryService interface {
	List(ctx context.Context, req *dto.ListInventoryReq) ([]*model.Inventory, *paging.Pagination, error)
	GetByProductID(ctx context.Context, productID string) (*model.Inventory, error)
	SetStock(ctx context.Context, req *dto.SetInventoryReq) (*model.Inventory, error)
	AdjustStock(ctx context.Context, productID string, req *dto.AdjustInventoryReq) (*model.Inventory, error)
	ConsumeStock(ctx context.Context, productID string, quantity int64) error
	Restock(ctx context.Context, productID string, quantity int64) error
}

type InventoryService struct {
	validator validation.Validation
	repo      repository.IInventoryRepository
}

func NewInventoryService(
	validator validation.Validation,
	repo repository.IInventoryRepository,
) *InventoryService {
	return &InventoryService{
		validator: validator,
		repo:      repo,
	}
}

func (s *InventoryService) List(ctx context.Context, req *dto.ListInventoryReq) ([]*model.Inventory, *paging.Pagination, error) {
	return s.repo.List(ctx, req)
}

func (s *InventoryService) GetByProductID(ctx context.Context, productID string) (*model.Inventory, error) {
	return s.getInventory(ctx, productID)
}

func (s *InventoryService) SetStock(ctx context.Context, req *dto.SetInventoryReq) (*model.Inventory, error) {
	if err := s.validator.ValidateStruct(req); err != nil {
		return nil, err
	}

	inventory, _, err := s.getOrCreate(ctx, req.ProductID)
	if err != nil {
		return nil, err
	}

	inventory.Quantity = req.Quantity
	if err := s.repo.Update(ctx, inventory); err != nil {
		return nil, err
	}

	return inventory, nil
}

func (s *InventoryService) AdjustStock(ctx context.Context, productID string, req *dto.AdjustInventoryReq) (*model.Inventory, error) {
	if err := s.validator.ValidateStruct(req); err != nil {
		return nil, err
	}

	inventory, _, err := s.getOrCreate(ctx, productID)
	if err != nil {
		return nil, err
	}

	newQuantity := inventory.Quantity + req.QuantityDelta
	if newQuantity < 0 {
		return nil, ErrInsufficientStock
	}

	inventory.Quantity = newQuantity
	if err := s.repo.Update(ctx, inventory); err != nil {
		return nil, err
	}

	return inventory, nil
}

func (s *InventoryService) ConsumeStock(ctx context.Context, productID string, quantity int64) error {
	if quantity < 0 {
		return errors.New("quantity must be positive")
	}

	inventory, err := s.getInventory(ctx, productID)
	if err != nil {
		return err
	}

	if inventory.Quantity < quantity {
		return ErrInsufficientStock
	}

	inventory.Quantity -= quantity
	return s.repo.Update(ctx, inventory)
}

func (s *InventoryService) Restock(ctx context.Context, productID string, quantity int64) error {
	if quantity < 0 {
		return errors.New("quantity must be positive")
	}

	inventory, _, err := s.getOrCreate(ctx, productID)
	if err != nil {
		return err
	}

	inventory.Quantity += quantity
	return s.repo.Update(ctx, inventory)
}

func (s *InventoryService) getInventory(ctx context.Context, productID string) (*model.Inventory, error) {
	inventory, err := s.repo.GetByProductID(ctx, productID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInventoryNotFound
		}
		return nil, err
	}

	return inventory, nil
}

func (s *InventoryService) getOrCreate(ctx context.Context, productID string) (*model.Inventory, bool, error) {
	inventory, err := s.repo.GetByProductID(ctx, productID)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, err
		}

		inv := &model.Inventory{ProductID: productID}
		if err := s.repo.Create(ctx, inv); err != nil {
			return nil, false, err
		}

		return inv, true, nil
	}

	return inventory, false, nil
}
