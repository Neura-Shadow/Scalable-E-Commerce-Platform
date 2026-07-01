package service

import (
	"context"
	"errors"
	"testing"

	"github.com/quangdangfit/gocommon/validation"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"goshop/internal/inventory/dto"
	"goshop/internal/inventory/model"
	repoMocks "goshop/internal/inventory/repository/mocks"
)

func TestInventoryServiceSetStockCreatesRecord(t *testing.T) {
	validator := validation.New()
	repo := &repoMocks.IInventoryRepository{}
	svc := NewInventoryService(validator, repo)

	repo.On("GetByProductID", mock.Anything, "product").Return(nil, gorm.ErrRecordNotFound).Once()
	repo.On("Create", mock.Anything, mock.MatchedBy(func(item *model.Inventory) bool {
		return item.ProductID == "product"
	})).Return(nil).Once()
	repo.On("Update", mock.Anything, mock.MatchedBy(func(item *model.Inventory) bool {
		return item.ProductID == "product" && item.Quantity == 10
	})).Return(nil).Once()

	res, err := svc.SetStock(context.Background(), &dto.SetInventoryReq{ProductID: "product", Quantity: 10})
	require.NoError(t, err)
	require.Equal(t, int64(10), res.Quantity)
	repo.AssertExpectations(t)
}

func TestInventoryServiceAdjustStockInsufficient(t *testing.T) {
	validator := validation.New()
	repo := &repoMocks.IInventoryRepository{}
	svc := NewInventoryService(validator, repo)

	repo.On("GetByProductID", mock.Anything, "product").Return(&model.Inventory{ProductID: "product", Quantity: 1}, nil).Once()

	res, err := svc.AdjustStock(context.Background(), "product", &dto.AdjustInventoryReq{QuantityDelta: -2})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInsufficientStock))
	require.Nil(t, res)
	repo.AssertExpectations(t)
}

func TestInventoryServiceConsumeStockSuccess(t *testing.T) {
	validator := validation.New()
	repo := &repoMocks.IInventoryRepository{}
	svc := NewInventoryService(validator, repo)

	repo.On("ConsumeStock", mock.Anything, "product", int64(3)).Return(true, nil).Once()

	err := svc.ConsumeStock(context.Background(), "product", 3)
	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestInventoryServiceConsumeStockInsufficient(t *testing.T) {
	validator := validation.New()
	repo := &repoMocks.IInventoryRepository{}
	svc := NewInventoryService(validator, repo)

	repo.On("ConsumeStock", mock.Anything, "product", int64(3)).Return(false, nil).Once()
	repo.On("GetByProductID", mock.Anything, "product").Return(&model.Inventory{ProductID: "product", Quantity: 1}, nil).Once()

	err := svc.ConsumeStock(context.Background(), "product", 3)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInsufficientStock))
	repo.AssertExpectations(t)
}

func TestInventoryServiceRestockCreatesRecord(t *testing.T) {
	validator := validation.New()
	repo := &repoMocks.IInventoryRepository{}
	svc := NewInventoryService(validator, repo)

	repo.On("Restock", mock.Anything, "product", int64(4)).Return(nil).Once()

	err := svc.Restock(context.Background(), "product", 4)
	require.NoError(t, err)
	repo.AssertExpectations(t)
}

func TestInventoryServiceConsumeStockNotFound(t *testing.T) {
	validator := validation.New()
	repo := &repoMocks.IInventoryRepository{}
	svc := NewInventoryService(validator, repo)

	repo.On("ConsumeStock", mock.Anything, "product", int64(3)).Return(false, nil).Once()
	repo.On("GetByProductID", mock.Anything, "product").Return(nil, gorm.ErrRecordNotFound).Once()

	err := svc.ConsumeStock(context.Background(), "product", 3)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInventoryNotFound))
	repo.AssertExpectations(t)
}
