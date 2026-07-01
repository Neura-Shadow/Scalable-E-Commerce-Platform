package service

import (
	"context"

	"github.com/quangdangfit/gocommon/validation"

	"goshop/internal/order/dto"
	"goshop/internal/order/model"
	"goshop/pkg/paging"
	"goshop/pkg/utils"
)

type OrderService struct {
	validator   validation.Validation
	repo        OrderRepository
	productRepo ProductRepository
	inventory   InventoryService
	transactor  Transactor
}

func NewOrderService(
	validator validation.Validation,
	repo OrderRepository,
	productRepo ProductRepository,
	inventory InventoryService,
	transactors ...Transactor,
) *OrderService {
	uow := staticUnitOfWork{
		orders:    repo,
		products:  productRepo,
		inventory: inventory,
	}
	transactor := Transactor(passthroughTransactor{uow: uow})
	if len(transactors) > 0 && transactors[0] != nil {
		transactor = transactors[0]
	}

	return &OrderService{
		validator:   validator,
		repo:        repo,
		productRepo: productRepo,
		inventory:   inventory,
		transactor:  transactor,
	}
}

func (s *OrderService) PlaceOrder(ctx context.Context, req *dto.PlaceOrderReq) (*model.Order, error) {
	if err := s.validator.ValidateStruct(req); err != nil {
		return nil, err
	}

	var order *model.Order
	err := s.transactor.WithinTransaction(ctx, func(uow UnitOfWork) error {
		var lines []*model.OrderLine
		utils.Copy(&lines, &req.Lines)

		productMap := make(map[string]*model.Product, len(lines))
		for _, line := range lines {
			product, err := uow.Products().GetProductByID(ctx, line.ProductID)
			if err != nil {
				return err
			}
			line.Price = product.Price * float64(line.Quantity)
			productMap[line.ProductID] = product
		}

		for _, line := range lines {
			if err := uow.Inventory().ConsumeStock(ctx, line.ProductID, int64(line.Quantity)); err != nil {
				return err
			}
		}

		created, err := uow.Orders().CreateOrder(ctx, req.UserID, lines)
		if err != nil {
			return err
		}

		for _, line := range created.Lines {
			line.Product = productMap[line.ProductID]
		}

		order = created
		return nil
	})
	if err != nil {
		return nil, err
	}

	return order, nil
}

type staticUnitOfWork struct {
	orders    OrderRepository
	products  ProductRepository
	inventory InventoryService
}

func (u staticUnitOfWork) Orders() OrderRepository {
	return u.orders
}

func (u staticUnitOfWork) Products() ProductRepository {
	return u.products
}

func (u staticUnitOfWork) Inventory() InventoryService {
	return u.inventory
}

type passthroughTransactor struct {
	uow UnitOfWork
}

func (t passthroughTransactor) WithinTransaction(_ context.Context, fn func(UnitOfWork) error) error {
	return fn(t.uow)
}

func (s *OrderService) GetOrderByID(ctx context.Context, id, userID string) (*model.Order, error) {
	order, err := s.repo.GetOrderByID(ctx, id, true)
	if err != nil {
		return nil, err
	}
	if userID == "" || order.UserID != userID {
		return nil, model.ErrPermissionDenied
	}

	return order, nil
}

func (s *OrderService) GetMyOrders(ctx context.Context, req *dto.ListOrderReq) ([]*model.Order, *paging.Pagination, error) {
	orders, pagination, err := s.repo.GetMyOrders(ctx, req)
	if err != nil {
		return nil, nil, err
	}

	return orders, pagination, err
}

func (s *OrderService) CancelOrder(ctx context.Context, orderID, userID string) (*model.Order, error) {
	var order *model.Order

	err := s.transactor.WithinTransaction(ctx, func(uow UnitOfWork) error {
		existingOrder, err := uow.Orders().GetOrderByID(ctx, orderID, true)
		if err != nil {
			return err
		}

		if userID != existingOrder.UserID {
			return model.ErrPermissionDenied
		}

		if existingOrder.Status == model.OrderStatusDone || existingOrder.Status == model.OrderStatusCancelled {
			return model.ErrInvalidOrderState
		}

		for _, line := range existingOrder.Lines {
			if err := uow.Inventory().Restock(ctx, line.ProductID, int64(line.Quantity)); err != nil {
				return err
			}
		}

		existingOrder.Status = model.OrderStatusCancelled
		if err := uow.Orders().UpdateOrder(ctx, existingOrder); err != nil {
			return err
		}

		order = existingOrder
		return nil
	})
	if err != nil {
		return nil, err
	}

	return order, nil
}
