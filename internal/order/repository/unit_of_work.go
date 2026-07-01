package repository

import (
	"context"

	"github.com/quangdangfit/gocommon/validation"

	inventoryRepository "goshop/internal/inventory/repository"
	inventoryService "goshop/internal/inventory/service"
	orderService "goshop/internal/order/service"
	outboxRepository "goshop/internal/outbox/repository"
	outboxService "goshop/internal/outbox/service"
	"goshop/pkg/dbs"
)

type UnitOfWork struct {
	db        dbs.IDatabase
	validator validation.Validation
}

func NewUnitOfWork(db dbs.IDatabase, validator validation.Validation) *UnitOfWork {
	return &UnitOfWork{
		db:        db,
		validator: validator,
	}
}

func (u *UnitOfWork) WithinTransaction(ctx context.Context, fn func(orderService.UnitOfWork) error) error {
	return u.db.WithTransactionContext(ctx, func(tx dbs.IDatabase) error {
		inventoryRepo := inventoryRepository.NewInventoryRepository(tx)
		outboxRepo := outboxRepository.NewOutboxRepository(tx)

		return fn(transactionalUnitOfWork{
			orders:    NewOrderRepository(tx),
			products:  NewProductRepository(tx),
			inventory: inventoryService.NewInventoryService(u.validator, inventoryRepo),
			outbox:    outboxService.NewOutboxService(outboxRepo),
		})
	})
}

type transactionalUnitOfWork struct {
	orders    orderService.OrderRepository
	products  orderService.ProductRepository
	inventory orderService.InventoryService
	outbox    orderService.OutboxService
}

func (u transactionalUnitOfWork) Orders() orderService.OrderRepository {
	return u.orders
}

func (u transactionalUnitOfWork) Products() orderService.ProductRepository {
	return u.products
}

func (u transactionalUnitOfWork) Inventory() orderService.InventoryService {
	return u.inventory
}

func (u transactionalUnitOfWork) Outbox() orderService.OutboxService {
	return u.outbox
}
