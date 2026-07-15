package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
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

func (r *OrderRepo) CreateOrder(ctx context.Context, order *model.Order, lines []*model.OrderLine) (*model.Order, error) {
	if err := r.createOrder(ctx, r.db, order, lines); err != nil {
		if isIdempotencyUniqueViolation(err) {
			return nil, model.ErrIdempotencyDuplicate
		}
		return nil, err
	}

	return order, nil
}

func (r *OrderRepo) GetOrderByIdempotencyKey(ctx context.Context, userID, keyHash string) (*model.Order, error) {
	var order model.Order
	err := r.db.FindOne(
		ctx,
		&order,
		dbs.WithPreload([]string{"Lines", "Lines.Product"}),
		dbs.WithQuery(
			dbs.NewQuery("user_id = ?", userID),
			dbs.NewQuery("idempotency_key = ?", keyHash),
		),
	)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, model.ErrOrderNotFound
	}
	if err != nil {
		return nil, err
	}
	return &order, nil
}

func isIdempotencyUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == "idx_orders_user_idempotency_key"
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

func (r *OrderRepo) CancelOrderIfCancellable(ctx context.Context, orderID, userID string) error {
	ctx, cancel := context.WithTimeout(ctx, dbs.DatabaseTimeout)
	defer cancel()

	result := r.db.GetDB().WithContext(ctx).
		Model(&model.Order{}).
		Where("id = ? AND user_id = ? AND status IN ?", orderID, userID, []model.OrderStatus{
			model.OrderStatusNew,
			model.OrderStatusInProgress,
		}).
		Updates(map[string]any{
			"status":     model.OrderStatusCancelled,
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return model.ErrInvalidOrderState
	}
	return nil
}
