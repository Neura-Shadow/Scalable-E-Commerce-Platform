package repository

import (
	"context"

	outboxService "goshop/internal/outbox/service"
	"goshop/pkg/dbs"
)

type Transactor struct {
	db dbs.IDatabase
}

func NewTransactor(db dbs.IDatabase) *Transactor {
	return &Transactor{db: db}
}

func (t *Transactor) WithinTransaction(ctx context.Context, fn func(outboxService.OutboxRepository) error) error {
	return t.db.WithTransactionContext(ctx, func(tx dbs.IDatabase) error {
		return fn(NewOutboxRepository(tx))
	})
}
