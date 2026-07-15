package service

import (
	"context"
	"errors"
	"testing"

	"github.com/quangdangfit/gocommon/validation"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"goshop/internal/cart/dto"
	"goshop/internal/cart/model"
	serviceMocks "goshop/internal/cart/service/mocks"
)

func TestCartServiceGetCartDelegatesToAtomicGetOrCreate(t *testing.T) {
	repository := serviceMocks.NewCartRepository(t)
	service := NewCartService(validation.New(), repository)
	expected := &model.Cart{ID: "cart-1", UserID: "user-1"}
	repository.On("GetOrCreateCart", mock.Anything, "user-1").Return(expected, nil).Once()

	actual, err := service.GetCartByUserID(context.Background(), "user-1")

	require.NoError(t, err)
	require.Same(t, expected, actual)
}

func TestCartServiceGetCartPropagatesRepositoryError(t *testing.T) {
	repository := serviceMocks.NewCartRepository(t)
	service := NewCartService(validation.New(), repository)
	expectedErr := errors.New("database unavailable")
	repository.On("GetOrCreateCart", mock.Anything, "user-1").Return(nil, expectedErr).Once()

	actual, err := service.GetCartByUserID(context.Background(), "user-1")

	require.Nil(t, actual)
	require.ErrorIs(t, err, expectedErr)
}

func TestCartServiceAddProductDelegatesAtomicMutation(t *testing.T) {
	repository := serviceMocks.NewCartRepository(t)
	service := NewCartService(validation.New(), repository)
	req := &dto.AddProductReq{
		UserID: "user-1",
		Line: &dto.CartLineReq{
			ProductID: "product-1",
			Quantity:  3,
		},
	}
	expected := &model.Cart{
		ID:     "cart-1",
		UserID: "user-1",
		Lines: []*model.CartLine{
			{ProductID: "product-1", Quantity: 3},
		},
	}
	repository.On("AddProduct", mock.Anything, "user-1", "product-1", uint(3)).Return(expected, nil).Once()

	actual, err := service.AddProduct(context.Background(), req)

	require.NoError(t, err)
	require.Same(t, expected, actual)
}

func TestCartServiceAddProductRejectsInvalidRequests(t *testing.T) {
	tests := map[string]*dto.AddProductReq{
		"missing user": {
			Line: &dto.CartLineReq{ProductID: "product-1", Quantity: 1},
		},
		"missing line": {
			UserID: "user-1",
		},
		"missing product": {
			UserID: "user-1",
			Line:   &dto.CartLineReq{Quantity: 1},
		},
		"zero quantity": {
			UserID: "user-1",
			Line:   &dto.CartLineReq{ProductID: "product-1"},
		},
	}

	for name, req := range tests {
		t.Run(name, func(t *testing.T) {
			repository := serviceMocks.NewCartRepository(t)
			service := NewCartService(validation.New(), repository)

			actual, err := service.AddProduct(context.Background(), req)

			require.Nil(t, actual)
			require.Error(t, err)
		})
	}
}

func TestCartServiceAddProductPropagatesRepositoryError(t *testing.T) {
	repository := serviceMocks.NewCartRepository(t)
	service := NewCartService(validation.New(), repository)
	expectedErr := errors.New("database unavailable")
	req := &dto.AddProductReq{
		UserID: "user-1",
		Line:   &dto.CartLineReq{ProductID: "product-1", Quantity: 1},
	}
	repository.On("AddProduct", mock.Anything, "user-1", "product-1", uint(1)).Return(nil, expectedErr).Once()

	actual, err := service.AddProduct(context.Background(), req)

	require.Nil(t, actual)
	require.ErrorIs(t, err, expectedErr)
}

func TestCartServiceRemoveProductDelegatesAtomicMutation(t *testing.T) {
	repository := serviceMocks.NewCartRepository(t)
	service := NewCartService(validation.New(), repository)
	req := &dto.RemoveProductReq{UserID: "user-1", ProductID: "product-1"}
	expected := &model.Cart{ID: "cart-1", UserID: "user-1"}
	repository.On("RemoveProduct", mock.Anything, "user-1", "product-1").Return(expected, nil).Once()

	actual, err := service.RemoveProduct(context.Background(), req)

	require.NoError(t, err)
	require.Same(t, expected, actual)
}

func TestCartServiceRemoveProductRejectsInvalidRequests(t *testing.T) {
	tests := map[string]*dto.RemoveProductReq{
		"missing user":    {ProductID: "product-1"},
		"missing product": {UserID: "user-1"},
	}

	for name, req := range tests {
		t.Run(name, func(t *testing.T) {
			repository := serviceMocks.NewCartRepository(t)
			service := NewCartService(validation.New(), repository)

			actual, err := service.RemoveProduct(context.Background(), req)

			require.Nil(t, actual)
			require.Error(t, err)
		})
	}
}

func TestCartServiceRemoveProductPropagatesRepositoryError(t *testing.T) {
	repository := serviceMocks.NewCartRepository(t)
	service := NewCartService(validation.New(), repository)
	expectedErr := errors.New("database unavailable")
	req := &dto.RemoveProductReq{UserID: "user-1", ProductID: "product-1"}
	repository.On("RemoveProduct", mock.Anything, "user-1", "product-1").Return(nil, expectedErr).Once()

	actual, err := service.RemoveProduct(context.Background(), req)

	require.Nil(t, actual)
	require.ErrorIs(t, err, expectedErr)
}
