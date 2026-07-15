package service

import (
	"context"
	"errors"
	"math"
	"sort"

	"github.com/quangdangfit/gocommon/validation"

	"goshop/internal/order/dto"
	"goshop/internal/order/model"
	appMetrics "goshop/pkg/metrics"
	"goshop/pkg/money"
	"goshop/pkg/paging"
	"goshop/pkg/utils"
)

const (
	orderAggregateType    = "order"
	OrderCreatedEventType = "order.created"
)

type OrderCreatedPayload struct {
	OrderID    string                    `json:"order_id"`
	UserID     string                    `json:"user_id"`
	TotalPrice float64                   `json:"total_price"`
	Status     model.OrderStatus         `json:"status"`
	Lines      []OrderCreatedLinePayload `json:"lines"`
}

type OrderCreatedLinePayload struct {
	ProductID string  `json:"product_id"`
	Quantity  uint    `json:"quantity"`
	Price     float64 `json:"price"`
}

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
	transactor Transactor,
) *OrderService {
	if transactor == nil {
		panic("order transactor is required")
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
	if err := validateOrderQuantities(req.Lines); err != nil {
		return nil, err
	}
	if err := s.validator.ValidateStruct(req); err != nil {
		return nil, err
	}
	if req.IdempotencyKeyHash != "" || req.IdempotencyFingerprint != "" {
		if req.IdempotencyKeyHash == "" || req.IdempotencyFingerprint == "" {
			return nil, model.ErrIdempotencyConflict
		}
		if existing, err := s.replayIdempotentOrder(ctx, req); !errors.Is(err, model.ErrOrderNotFound) {
			return existing, err
		}
	}

	var order *model.Order
	var outboxCreated bool
	err := s.transactor.WithinTransaction(ctx, func(uow UnitOfWork) error {
		var lines []*model.OrderLine
		utils.Copy(&lines, &req.Lines)

		productMap := make(map[string]*model.Product, len(lines))
		var totalMinorUnits int64
		for _, line := range lines {
			product, err := uow.Products().GetProductByID(ctx, line.ProductID)
			if err != nil {
				return err
			}
			unitPrice, err := money.ToMinorUnits(product.Price)
			if err != nil {
				return model.ErrInvalidOrderAmount
			}
			lineTotal, err := money.Multiply(unitPrice, line.Quantity)
			if err != nil {
				return model.ErrInvalidOrderAmount
			}
			totalMinorUnits, err = money.Add(totalMinorUnits, lineTotal)
			if err != nil {
				return model.ErrInvalidOrderAmount
			}
			line.Price = money.FromMinorUnits(lineTotal)
			productMap[line.ProductID] = product
		}

		plan, err := stockConsumptionPlan(lines)
		if err != nil {
			return err
		}
		for _, item := range plan {
			if err := uow.Inventory().ConsumeStock(ctx, item.productID, item.quantity); err != nil {
				return err
			}
		}

		createdOrder := &model.Order{
			UserID:     req.UserID,
			TotalPrice: money.FromMinorUnits(totalMinorUnits),
		}
		if req.IdempotencyKeyHash != "" {
			createdOrder.IdempotencyKey = &req.IdempotencyKeyHash
			createdOrder.IdempotencyFingerprint = &req.IdempotencyFingerprint
		}

		created, err := uow.Orders().CreateOrder(ctx, createdOrder, lines)
		if err != nil {
			return err
		}

		for _, line := range created.Lines {
			line.Product = productMap[line.ProductID]
		}

		if outbox := uow.Outbox(); outbox != nil {
			if _, err := outbox.CreatePending(ctx, orderAggregateType, created.ID, OrderCreatedEventType, buildOrderCreatedPayload(created)); err != nil {
				return err
			}
			outboxCreated = true
		}

		order = created
		return nil
	})
	if err != nil {
		if errors.Is(err, model.ErrIdempotencyDuplicate) {
			return s.replayIdempotentOrder(ctx, req)
		}
		return nil, err
	}
	if outboxCreated {
		appMetrics.RecordOutboxEventCreated(OrderCreatedEventType)
	}

	return order, nil
}

func (s *OrderService) replayIdempotentOrder(ctx context.Context, req *dto.PlaceOrderReq) (*model.Order, error) {
	existing, err := s.repo.GetOrderByIdempotencyKey(ctx, req.UserID, req.IdempotencyKeyHash)
	if err != nil {
		return nil, err
	}
	if existing.IdempotencyFingerprint == nil || *existing.IdempotencyFingerprint != req.IdempotencyFingerprint {
		return nil, model.ErrIdempotencyConflict
	}
	existing.IdempotencyReplay = true
	return existing, nil
}

type stockConsumption struct {
	productID string
	quantity  int64
}

func validateOrderQuantities(lines []dto.PlaceOrderLineReq) error {
	for _, line := range lines {
		if line.Quantity == 0 || line.Quantity > dto.MaxOrderLineQuantity {
			return model.ErrInvalidOrderQuantity
		}
	}
	return nil
}

func stockConsumptionPlan(lines []*model.OrderLine) ([]stockConsumption, error) {
	quantities := make(map[string]int64, len(lines))
	for _, line := range lines {
		if uint64(line.Quantity) > math.MaxInt64 {
			return nil, model.ErrInvalidOrderQuantity
		}
		quantity := int64(line.Quantity)
		if quantities[line.ProductID] > math.MaxInt64-quantity {
			return nil, model.ErrInvalidOrderQuantity
		}
		quantities[line.ProductID] += quantity
	}

	productIDs := make([]string, 0, len(quantities))
	for productID := range quantities {
		productIDs = append(productIDs, productID)
	}
	sort.Strings(productIDs)

	plan := make([]stockConsumption, 0, len(productIDs))
	for _, productID := range productIDs {
		plan = append(plan, stockConsumption{
			productID: productID,
			quantity:  quantities[productID],
		})
	}
	return plan, nil
}

type staticUnitOfWork struct {
	orders    OrderRepository
	products  ProductRepository
	inventory InventoryService
	outbox    OutboxService
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

func (u staticUnitOfWork) Outbox() OutboxService {
	return u.outbox
}

type passthroughTransactor struct {
	uow UnitOfWork
}

func (t passthroughTransactor) WithinTransaction(_ context.Context, fn func(UnitOfWork) error) error {
	return fn(t.uow)
}

func buildOrderCreatedPayload(order *model.Order) OrderCreatedPayload {
	payload := OrderCreatedPayload{
		OrderID:    order.ID,
		UserID:     order.UserID,
		TotalPrice: order.TotalPrice,
		Status:     order.Status,
		Lines:      make([]OrderCreatedLinePayload, 0, len(order.Lines)),
	}
	for _, line := range order.Lines {
		payload.Lines = append(payload.Lines, OrderCreatedLinePayload{
			ProductID: line.ProductID,
			Quantity:  line.Quantity,
			Price:     line.Price,
		})
	}

	return payload
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
		if err := uow.Orders().CancelOrderIfCancellable(ctx, orderID, userID); err != nil {
			return err
		}

		restockPlan, err := stockConsumptionPlan(existingOrder.Lines)
		if err != nil {
			return err
		}
		for _, item := range restockPlan {
			if err := uow.Inventory().Restock(ctx, item.productID, item.quantity); err != nil {
				return err
			}
		}

		existingOrder.Status = model.OrderStatusCancelled
		order = existingOrder
		return nil
	})
	if err != nil {
		return nil, err
	}

	return order, nil
}
