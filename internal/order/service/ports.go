package service

import (
	"context"

	"goshop/internal/order/dto"
	"goshop/internal/order/model"
	"goshop/pkg/paging"
)

//go:generate mockery --name=IOrderService
//go:generate mockery --name=OrderRepository
//go:generate mockery --name=ProductRepository
//go:generate mockery --name=InventoryService
type IOrderService interface {
	PlaceOrder(ctx context.Context, req *dto.PlaceOrderReq) (*model.Order, error)
	GetOrderByID(ctx context.Context, id, userID string) (*model.Order, error)
	GetMyOrders(ctx context.Context, req *dto.ListOrderReq) ([]*model.Order, *paging.Pagination, error)
	CancelOrder(ctx context.Context, orderID, userID string) (*model.Order, error)
}

type Transactor interface {
	WithinTransaction(ctx context.Context, fn func(UnitOfWork) error) error
}

type UnitOfWork interface {
	Orders() OrderRepository
	Products() ProductRepository
	Inventory() InventoryService
}

type OrderRepository interface {
	CreateOrder(ctx context.Context, userID string, lines []*model.OrderLine) (*model.Order, error)
	GetOrderByID(ctx context.Context, id string, preload bool) (*model.Order, error)
	GetMyOrders(ctx context.Context, req *dto.ListOrderReq) ([]*model.Order, *paging.Pagination, error)
	UpdateOrder(ctx context.Context, order *model.Order) error
}

type ProductRepository interface {
	GetProductByID(ctx context.Context, id string) (*model.Product, error)
}

type InventoryService interface {
	ConsumeStock(ctx context.Context, productID string, quantity int64) error
	Restock(ctx context.Context, productID string, quantity int64) error
}
