package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"goshop/internal/cart/model"
	"goshop/pkg/dbs/mocks"
)

func TestCartRepositoryTransactionErrorsArePropagated(t *testing.T) {
	tests := map[string]func(context.Context, ICartRepository) (*model.Cart, error){
		"get or create": func(ctx context.Context, repository ICartRepository) (*model.Cart, error) {
			return repository.GetOrCreateCart(ctx, "user-1")
		},
		"add product": func(ctx context.Context, repository ICartRepository) (*model.Cart, error) {
			return repository.AddProduct(ctx, "user-1", "product-1", 1)
		},
		"remove product": func(ctx context.Context, repository ICartRepository) (*model.Cart, error) {
			return repository.RemoveProduct(ctx, "user-1", "product-1")
		},
	}

	for name, operation := range tests {
		t.Run(name, func(t *testing.T) {
			database := mocks.NewIDatabase(t)
			repository := NewCartRepository(database)
			expectedErr := errors.New("transaction failed")
			database.On("WithTransactionContext", mock.Anything, mock.Anything).Return(expectedErr).Once()

			cart, err := operation(context.Background(), repository)

			require.Nil(t, cart)
			require.ErrorIs(t, err, expectedErr)
		})
	}
}

func TestCartRepositoryGetByUserID(t *testing.T) {
	database := mocks.NewIDatabase(t)
	repository := NewCartRepository(database)
	database.On("FindOne", mock.Anything, &model.Cart{}, mock.Anything, mock.Anything).Return(nil).Once()

	cart, err := repository.GetCartByUserID(context.Background(), "user-1")

	require.NoError(t, err)
	require.NotNil(t, cart)
}

func TestCartRepositoryGetByUserIDPropagatesDatabaseError(t *testing.T) {
	database := mocks.NewIDatabase(t)
	repository := NewCartRepository(database)
	expectedErr := errors.New("database unavailable")
	database.On("FindOne", mock.Anything, &model.Cart{}, mock.Anything, mock.Anything).Return(expectedErr).Once()

	cart, err := repository.GetCartByUserID(context.Background(), "user-1")

	require.Nil(t, cart)
	require.ErrorIs(t, err, expectedErr)
}

func TestCartRepositoryGetByUserIDMapsNotFound(t *testing.T) {
	database := mocks.NewIDatabase(t)
	repository := NewCartRepository(database)
	database.On("FindOne", mock.Anything, &model.Cart{}, mock.Anything, mock.Anything).Return(gorm.ErrRecordNotFound).Once()

	cart, err := repository.GetCartByUserID(context.Background(), "user-1")

	require.Nil(t, cart)
	require.ErrorIs(t, err, model.ErrCartNotFound)
}
