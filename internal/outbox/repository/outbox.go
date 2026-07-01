package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"goshop/internal/outbox/model"
	"goshop/pkg/dbs"
)

//go:generate mockery --name=IOutboxRepository
type IOutboxRepository interface {
	CreatePending(ctx context.Context, event *model.OutboxEvent) error
	ListPendingReady(ctx context.Context, now time.Time, limit int) ([]*model.OutboxEvent, error)
	ListPendingReadyLocked(ctx context.Context, now time.Time, limit int) ([]*model.OutboxEvent, error)
	MarkPublished(ctx context.Context, eventID string, publishedAt time.Time) error
	MarkPublishFailed(ctx context.Context, eventID string, nextAttemptAt time.Time) error
	MarkDeadLetter(ctx context.Context, eventID string) error
}

type OutboxRepo struct {
	db dbs.IDatabase
}

func NewOutboxRepository(db dbs.IDatabase) *OutboxRepo {
	return &OutboxRepo{db: db}
}

func (r *OutboxRepo) CreatePending(ctx context.Context, event *model.OutboxEvent) error {
	event.Status = model.OutboxEventStatusPending
	return r.db.Create(ctx, event)
}

func (r *OutboxRepo) ListPendingReady(ctx context.Context, now time.Time, limit int) ([]*model.OutboxEvent, error) {
	if limit <= 0 {
		limit = 100
	}

	var events []*model.OutboxEvent
	err := r.db.Find(
		ctx,
		&events,
		dbs.WithQuery(dbs.NewQuery("status = ? AND next_attempt_at <= ?", model.OutboxEventStatusPending, now)),
		dbs.WithOrder("created_at"),
		dbs.WithLimit(limit),
	)
	if err != nil {
		return nil, err
	}

	return events, nil
}

func (r *OutboxRepo) ListPendingReadyLocked(ctx context.Context, now time.Time, limit int) ([]*model.OutboxEvent, error) {
	var events []*model.OutboxEvent
	if err := pendingReadyQuery(r.db.GetDB(), ctx, now, limit, true).Find(&events).Error; err != nil {
		return nil, err
	}

	return events, nil
}

func (r *OutboxRepo) MarkPublished(ctx context.Context, eventID string, publishedAt time.Time) error {
	event, err := r.getByID(ctx, eventID)
	if err != nil {
		return err
	}

	event.Status = model.OutboxEventStatusPublished
	event.PublishedAt = &publishedAt
	return r.db.Update(ctx, event)
}

func (r *OutboxRepo) MarkPublishFailed(ctx context.Context, eventID string, nextAttemptAt time.Time) error {
	event, err := r.getByID(ctx, eventID)
	if err != nil {
		return err
	}

	event.Attempts++
	event.Status = model.OutboxEventStatusPending
	event.NextAttemptAt = nextAttemptAt
	return r.db.Update(ctx, event)
}

func (r *OutboxRepo) MarkDeadLetter(ctx context.Context, eventID string) error {
	event, err := r.getByID(ctx, eventID)
	if err != nil {
		return err
	}

	event.Status = model.OutboxEventStatusDeadLetter
	return r.db.Update(ctx, event)
}

func (r *OutboxRepo) getByID(ctx context.Context, eventID string) (*model.OutboxEvent, error) {
	var event model.OutboxEvent
	if err := r.db.FindById(ctx, eventID, &event); err != nil {
		return nil, err
	}

	return &event, nil
}

func pendingReadyQuery(db *gorm.DB, ctx context.Context, now time.Time, limit int, locked bool) *gorm.DB {
	if limit <= 0 {
		limit = 100
	}

	query := db.WithContext(ctx).
		Where("status = ? AND next_attempt_at <= ?", model.OutboxEventStatusPending, now).
		Order("created_at").
		Limit(limit)
	if locked {
		query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
	}

	return query
}
