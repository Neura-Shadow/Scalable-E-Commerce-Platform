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
}

func NewOrderService(
	validator validation.Validation,
	repo OrderRepository,
	productRepo ProductRepository,
	inventory InventoryService,
) *OrderService {
	return &OrderService{
		validator:   validator,
		repo:        repo,
		productRepo: productRepo,
		inventory:   inventory,
	}
}

func (s *OrderService) PlaceOrder(ctx context.Context, req *dto.PlaceOrderReq) (*model.Order, error) {
	if err := s.validator.ValidateStruct(req); err != nil {
		return nil, err
	}

	var lines []*model.OrderLine
	utils.Copy(&lines, &req.Lines)

	productMap := make(map[string]*model.Product)
	for _, line := range lines {
		product, err := s.productRepo.GetProductByID(ctx, line.ProductID)
		if err != nil {
			return nil, err
		}
		line.Price = product.Price * float64(line.Quantity)
		productMap[line.ProductID] = product
	}

	consumed := make([]*model.OrderLine, 0, len(lines))
	for _, line := range lines {
		if err := s.inventory.ConsumeStock(ctx, line.ProductID, int64(line.Quantity)); err != nil {
			for _, consumedLine := range consumed {
				_ = s.inventory.Restock(ctx, consumedLine.ProductID, int64(consumedLine.Quantity))
			}
			return nil, err
		}
		consumed = append(consumed, line)
	}

	order, err := s.repo.CreateOrder(ctx, req.UserID, lines)
	if err != nil {
		for _, consumedLine := range consumed {
			_ = s.inventory.Restock(ctx, consumedLine.ProductID, int64(consumedLine.Quantity))
		}
		return nil, err
	}

	for _, line := range order.Lines {
		line.Product = productMap[line.ProductID]
	}

	return order, nil
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
	order, err := s.repo.GetOrderByID(ctx, orderID, true)
	if err != nil {
		return nil, err
	}

	if userID != order.UserID {
		return nil, model.ErrPermissionDenied
	}

	if order.Status == model.OrderStatusDone || order.Status == model.OrderStatusCancelled {
		return nil, model.ErrInvalidOrderState
	}

	restocked := make([]*model.OrderLine, 0, len(order.Lines))
	for _, line := range order.Lines {
		if err := s.inventory.Restock(ctx, line.ProductID, int64(line.Quantity)); err != nil {
			for _, restored := range restocked {
				_ = s.inventory.ConsumeStock(ctx, restored.ProductID, int64(restored.Quantity))
			}
			return nil, err
		}
		restocked = append(restocked, line)
	}

	order.Status = model.OrderStatusCancelled
	err = s.repo.UpdateOrder(ctx, order)
	if err != nil {
		for _, restored := range restocked {
			_ = s.inventory.ConsumeStock(ctx, restored.ProductID, int64(restored.Quantity))
		}
		return nil, err
	}

	return order, nil
}
