package repository

import (
	"context"

	"gorm.io/gorm/clause"

	"goshop/internal/order/dto"
	"goshop/internal/order/model"
	"goshop/pkg/dbs"
	"goshop/pkg/paging"
	"goshop/pkg/utils"
)

type OrderRepo struct {
	db dbs.IDatabase
}

var orderListOrderColumns = map[string]string{
	"code":        "code",
	"created_at":  "created_at",
	"status":      "status",
	"total_price": "total_price",
	"updated_at":  "updated_at",
}

func NewOrderRepository(db dbs.IDatabase) *OrderRepo {
	return &OrderRepo{db: db}
}

func (r *OrderRepo) CreateOrder(ctx context.Context, userID string, lines []*model.OrderLine) (*model.Order, error) {
	order := new(model.Order)

	var totalPrice float64
	for _, line := range lines {
		totalPrice += line.Price
	}
	order.TotalPrice = totalPrice
	order.UserID = userID

	if err := r.createOrder(ctx, r.db, order, lines); err != nil {
		return nil, err
	}

	return order, nil
}

func (r *OrderRepo) createOrder(ctx context.Context, tx dbs.IDatabase, order *model.Order, lines []*model.OrderLine) error {
	// Create Order
	if err := tx.Create(ctx, order); err != nil {
		return err
	}

	// Create order lines
	for _, line := range lines {
		line.OrderID = order.ID
	}
	if err := tx.CreateInBatches(ctx, &lines, len(lines)); err != nil {
		return err
	}

	utils.Copy(&order.Lines, &lines)
	return nil
}

func (r *OrderRepo) GetOrderByID(ctx context.Context, id string, preload bool) (*model.Order, error) {
	var order model.Order
	opts := []dbs.FindOption{
		dbs.WithQuery(dbs.NewQuery("id = ?", id)),
	}
	if preload {
		opts = append(opts, dbs.WithPreload([]string{"Lines", "Lines.Product"}))
	}

	if err := r.db.FindOne(ctx, &order, opts...); err != nil {
		return nil, err
	}

	return &order, nil
}

func (r *OrderRepo) GetMyOrders(ctx context.Context, req *dto.ListOrderReq) ([]*model.Order, *paging.Pagination, error) {
	query := []dbs.Query{
		dbs.NewQuery("user_id = ?", req.UserID),
	}
	if req.Code != "" {
		query = append(query, dbs.NewQuery("code = ?", req.Code))
	}
	if req.Status != "" {
		query = append(query, dbs.NewQuery("status = ?", req.Status))
	}

	order := orderListOrder(req.OrderBy, req.OrderDesc)

	var total int64
	if err := r.db.Count(ctx, &model.Order{}, &total, dbs.WithQuery(query...)); err != nil {
		return nil, nil, err
	}

	pagination := paging.New(req.Page, req.Limit, total)

	var orders []*model.Order
	if err := r.db.Find(
		ctx,
		&orders,
		dbs.WithPreload([]string{"Lines", "Lines.Product"}),
		dbs.WithQuery(query...),
		dbs.WithLimit(int(pagination.Limit)),
		dbs.WithOffset(int(pagination.Skip)),
		dbs.WithOrder(order),
	); err != nil {
		return nil, nil, err
	}

	return orders, pagination, nil
}

func orderListOrder(orderBy string, desc bool) clause.OrderByColumn {
	return dbs.SafeOrderByColumn(orderBy, desc, "created_at", orderListOrderColumns)
}

func (r *OrderRepo) UpdateOrder(ctx context.Context, order *model.Order) error {
	return r.db.Update(ctx, order)
}
