package repository

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"goshop/internal/inventory/dto"
	"goshop/internal/inventory/model"
	"goshop/pkg/config"
	"goshop/pkg/dbs"
	"goshop/pkg/paging"
)

//go:generate mockery --name=IInventoryRepository
type IInventoryRepository interface {
	List(ctx context.Context, req *dto.ListInventoryReq) ([]*model.Inventory, *paging.Pagination, error)
	GetByProductID(ctx context.Context, productID string) (*model.Inventory, error)
	Create(ctx context.Context, inventory *model.Inventory) error
	Update(ctx context.Context, inventory *model.Inventory) error
	ConsumeStock(ctx context.Context, productID string, quantity int64) (bool, error)
	Restock(ctx context.Context, productID string, quantity int64) error
}

type InventoryRepo struct {
	db dbs.IDatabase
}

var inventoryListOrderColumns = map[string]string{
	"created_at": "created_at",
	"product_id": "product_id",
	"quantity":   "quantity",
	"updated_at": "updated_at",
}

func NewInventoryRepository(db dbs.IDatabase) *InventoryRepo {
	return &InventoryRepo{db: db}
}

func (r *InventoryRepo) List(ctx context.Context, req *dto.ListInventoryReq) ([]*model.Inventory, *paging.Pagination, error) {
	ctx, cancel := context.WithTimeout(ctx, config.DatabaseTimeout)
	defer cancel()

	query := make([]dbs.Query, 0)
	if req.ProductID != "" {
		query = append(query, dbs.NewQuery("product_id = ?", req.ProductID))
	}

	order := inventoryListOrder(req.OrderBy, req.OrderDesc)

	var total int64
	if err := r.db.Count(ctx, &model.Inventory{}, &total, dbs.WithQuery(query...)); err != nil {
		return nil, nil, err
	}

	pagination := paging.New(req.Page, req.Limit, total)

	var items []*model.Inventory
	if err := r.db.Find(
		ctx,
		&items,
		dbs.WithQuery(query...),
		dbs.WithLimit(int(pagination.Limit)),
		dbs.WithOffset(int(pagination.Skip)),
		dbs.WithOrder(order),
	); err != nil {
		return nil, nil, err
	}

	return items, pagination, nil
}

func inventoryListOrder(orderBy string, desc bool) clause.OrderByColumn {
	return dbs.SafeOrderByColumn(orderBy, desc, "created_at", inventoryListOrderColumns)
}

func (r *InventoryRepo) GetByProductID(ctx context.Context, productID string) (*model.Inventory, error) {
	var item model.Inventory
	if err := r.db.FindOne(ctx, &item, dbs.WithQuery(dbs.NewQuery("product_id = ?", productID))); err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *InventoryRepo) Create(ctx context.Context, inventory *model.Inventory) error {
	return r.db.Create(ctx, inventory)
}

func (r *InventoryRepo) Update(ctx context.Context, inventory *model.Inventory) error {
	return r.db.Update(ctx, inventory)
}

func (r *InventoryRepo) ConsumeStock(ctx context.Context, productID string, quantity int64) (bool, error) {
	result := r.db.GetDB().
		WithContext(ctx).
		Model(&model.Inventory{}).
		Where("product_id = ? AND quantity >= ?", productID, quantity).
		UpdateColumn("quantity", gorm.Expr("quantity - ?", quantity))
	if result.Error != nil {
		return false, result.Error
	}

	return result.RowsAffected > 0, nil
}

func (r *InventoryRepo) Restock(ctx context.Context, productID string, quantity int64) error {
	return r.db.GetDB().
		WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "product_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"quantity": gorm.Expr("inventories.quantity + ?", quantity),
			}),
		}).
		Create(&model.Inventory{
			ProductID: productID,
			Quantity:  quantity,
		}).Error
}
