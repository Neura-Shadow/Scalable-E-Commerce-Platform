package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/datatypes"

	"goshop/internal/outbox/model"
)

const (
	defaultRetryDelay  = time.Minute
	defaultMaxAttempts = 3
)

type IOutboxService interface {
	CreatePending(ctx context.Context, aggregateType, aggregateID, eventType string, payload any) (*model.OutboxEvent, error)
	ListPendingReady(ctx context.Context, limit int) ([]*model.OutboxEvent, error)
	ListPendingReadyLocked(ctx context.Context, limit int) ([]*model.OutboxEvent, error)
	MarkPublished(ctx context.Context, eventID string) error
	RecordPublishFailure(ctx context.Context, event *model.OutboxEvent) error
	Publish(ctx context.Context, publisher EventPublisher, event *model.OutboxEvent) error
}

type OutboxRepository interface {
	CreatePending(ctx context.Context, event *model.OutboxEvent) error
	ListPendingReady(ctx context.Context, now time.Time, limit int) ([]*model.OutboxEvent, error)
	ListPendingReadyLocked(ctx context.Context, now time.Time, limit int) ([]*model.OutboxEvent, error)
	MarkPublished(ctx context.Context, eventID string, publishedAt time.Time) error
	MarkPublishFailed(ctx context.Context, eventID string, nextAttemptAt time.Time) error
	MarkDeadLetter(ctx context.Context, eventID string) error
}

type EventPublisher interface {
	Publish(ctx context.Context, event *model.OutboxEvent) error
}

type OutboxService struct {
	repo        OutboxRepository
	now         func() time.Time
	retryDelay  time.Duration
	maxAttempts int
}

type Option func(*OutboxService)

func WithNow(now func() time.Time) Option {
	return func(s *OutboxService) {
		if now != nil {
			s.now = now
		}
	}
}

func WithRetryDelay(delay time.Duration) Option {
	return func(s *OutboxService) {
		if delay > 0 {
			s.retryDelay = delay
		}
	}
}

func WithMaxAttempts(maxAttempts int) Option {
	return func(s *OutboxService) {
		if maxAttempts > 0 {
			s.maxAttempts = maxAttempts
		}
	}
}

func NewOutboxService(repo OutboxRepository, opts ...Option) *OutboxService {
	svc := &OutboxService{
		repo:        repo,
		now:         time.Now,
		retryDelay:  defaultRetryDelay,
		maxAttempts: defaultMaxAttempts,
	}
	for _, opt := range opts {
		opt(svc)
	}

	return svc
}

func (s *OutboxService) CreatePending(ctx context.Context, aggregateType, aggregateID, eventType string, payload any) (*model.OutboxEvent, error) {
	if aggregateType == "" || aggregateID == "" || eventType == "" {
		return nil, errors.New("outbox event identity is required")
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	event := &model.OutboxEvent{
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		EventType:     eventType,
		Payload:       datatypes.JSON(payloadBytes),
		Status:        model.OutboxEventStatusPending,
		Attempts:      0,
		NextAttemptAt: s.now(),
	}
	if err := s.repo.CreatePending(ctx, event); err != nil {
		return nil, err
	}

	return event, nil
}

func (s *OutboxService) ListPendingReady(ctx context.Context, limit int) ([]*model.OutboxEvent, error) {
	return s.repo.ListPendingReady(ctx, s.now(), limit)
}

func (s *OutboxService) ListPendingReadyLocked(ctx context.Context, limit int) ([]*model.OutboxEvent, error) {
	return s.repo.ListPendingReadyLocked(ctx, s.now(), limit)
}

func (s *OutboxService) MarkPublished(ctx context.Context, eventID string) error {
	return s.repo.MarkPublished(ctx, eventID, s.now())
}

func (s *OutboxService) Publish(ctx context.Context, publisher EventPublisher, event *model.OutboxEvent) error {
	if publisher == nil {
		return errors.New("outbox publisher is required")
	}
	if event == nil {
		return errors.New("outbox event is required")
	}

	if err := publisher.Publish(ctx, event); err != nil {
		if recordErr := s.RecordPublishFailure(ctx, event); recordErr != nil {
			return recordErr
		}
		return err
	}

	return s.MarkPublished(ctx, event.ID)
}

func (s *OutboxService) RecordPublishFailure(ctx context.Context, event *model.OutboxEvent) error {
	if event == nil {
		return errors.New("outbox event is required")
	}

	attemptsAfterFailure := event.Attempts + 1
	nextAttemptAt := s.now().Add(time.Duration(attemptsAfterFailure) * s.retryDelay)
	if err := s.repo.MarkPublishFailed(ctx, event.ID, nextAttemptAt); err != nil {
		return err
	}
	if attemptsAfterFailure >= s.maxAttempts {
		return s.repo.MarkDeadLetter(ctx, event.ID)
	}

	return nil
}
