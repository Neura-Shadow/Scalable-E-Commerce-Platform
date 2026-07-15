package repository

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"goshop/internal/cart/model"
	"goshop/pkg/dbs"
)

//go:generate mockery --name=ICartRepository
type ICartRepository interface {
	GetOrCreateCart(ctx context.Context, userID string) (*model.Cart, error)
	AddProduct(ctx context.Context, userID, productID string, quantity uint) (*model.Cart, error)
	RemoveProduct(ctx context.Context, userID, productID string) (*model.Cart, error)
	GetCartByUserID(ctx context.Context, userID string) (*model.Cart, error)
}

type CartRepo struct {
	db dbs.IDatabase
}

func NewCartRepository(db dbs.IDatabase) *CartRepo {
	return &CartRepo{db: db}
}

func (r *CartRepo) GetOrCreateCart(ctx context.Context, userID string) (*model.Cart, error) {
	return r.withLockedCart(ctx, userID, nil)
}

func (r *CartRepo) AddProduct(ctx context.Context, userID, productID string, quantity uint) (*model.Cart, error) {
	return r.withLockedCart(ctx, userID, func(database *gorm.DB, cart *model.Cart) error {
		line := &model.CartLine{
			CartID:    cart.ID,
			ProductID: productID,
			Quantity:  quantity,
		}
		if err := database.
			Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "cart_id"}, {Name: "product_id"}},
				DoNothing: true,
			}).
			Omit("Product").
			Create(line).Error; err != nil {
			return fmt.Errorf("add cart product: %w", err)
		}
		return nil
	})
}

func (r *CartRepo) RemoveProduct(ctx context.Context, userID, productID string) (*model.Cart, error) {
	return r.withLockedCart(ctx, userID, func(database *gorm.DB, cart *model.Cart) error {
		if err := database.
			Where("cart_id = ? AND product_id = ?", cart.ID, productID).
			Delete(&model.CartLine{}).Error; err != nil {
			return fmt.Errorf("remove cart product: %w", err)
		}
		return nil
	})
}

func (r *CartRepo) GetCartByUserID(ctx context.Context, userID string) (*model.Cart, error) {
	var cart model.Cart
	opts := []dbs.FindOption{
		dbs.WithQuery(dbs.NewQuery("user_id = ?", userID)),
	}
	opts = append(opts, dbs.WithPreload([]string{"User", "Lines.Product"}))

	if err := r.db.FindOne(ctx, &cart, opts...); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, model.ErrCartNotFound
		}
		return nil, fmt.Errorf("get cart by user ID: %w", err)
	}

	return &cart, nil
}

func (r *CartRepo) withLockedCart(
	ctx context.Context,
	userID string,
	mutate func(database *gorm.DB, cart *model.Cart) error,
) (*model.Cart, error) {
	var result *model.Cart
	err := r.db.WithTransactionContext(ctx, func(tx dbs.IDatabase) error {
		database := tx.GetDB().WithContext(ctx)
		cart, err := getOrCreateLockedCart(database, userID)
		if err != nil {
			return err
		}
		if mutate != nil {
			if err := mutate(database, cart); err != nil {
				return err
			}
		}

		result, err = loadCart(database, userID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func getOrCreateLockedCart(database *gorm.DB, userID string) (*model.Cart, error) {
	candidate := &model.Cart{UserID: userID}
	if err := database.
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}},
			DoNothing: true,
		}).
		Omit("User", "Lines").
		Create(candidate).Error; err != nil {
		return nil, fmt.Errorf("ensure cart: %w", err)
	}

	var cart model.Cart
	if err := database.
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ?", userID).
		First(&cart).Error; err != nil {
		return nil, fmt.Errorf("lock cart: %w", err)
	}
	return &cart, nil
}

func loadCart(database *gorm.DB, userID string) (*model.Cart, error) {
	var cart model.Cart
	if err := database.
		Preload("User").
		Preload("Lines.Product").
		Where("user_id = ?", userID).
		First(&cart).Error; err != nil {
		return nil, fmt.Errorf("load cart: %w", err)
	}
	return &cart, nil
}
