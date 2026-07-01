package service

import (
	"context"
	"errors"
	"time"

	"github.com/quangdangfit/gocommon/logger"
)

const (
	defaultPublisherBatchSize       = 100
	defaultPublisherRetryBase       = time.Minute
	defaultPublisherMaxAttempts     = 3
	defaultPublisherIntervalSeconds = 30
)

type OutboxTransactor interface {
	WithinTransaction(ctx context.Context, fn func(OutboxRepository) error) error
}

type PublisherWorker struct {
	transactor  OutboxTransactor
	publisher   EventPublisher
	now         func() time.Time
	batchSize   int
	maxAttempts int
	retryBase   time.Duration
}

type PublisherWorkerOption func(*PublisherWorker)

type PublishBatchResult struct {
	Fetched      int
	Published    int
	Failed       int
	DeadLettered int
	Latency      time.Duration
}

func WithPublisherNow(now func() time.Time) PublisherWorkerOption {
	return func(w *PublisherWorker) {
		if now != nil {
			w.now = now
		}
	}
}

func WithPublisherBatchSize(size int) PublisherWorkerOption {
	return func(w *PublisherWorker) {
		if size > 0 {
			w.batchSize = size
		}
	}
}

func WithPublisherMaxAttempts(maxAttempts int) PublisherWorkerOption {
	return func(w *PublisherWorker) {
		if maxAttempts > 0 {
			w.maxAttempts = maxAttempts
		}
	}
}

func WithPublisherRetryBase(retryBase time.Duration) PublisherWorkerOption {
	return func(w *PublisherWorker) {
		if retryBase > 0 {
			w.retryBase = retryBase
		}
	}
}

func NewPublisherWorker(transactor OutboxTransactor, publisher EventPublisher, opts ...PublisherWorkerOption) *PublisherWorker {
	worker := &PublisherWorker{
		transactor:  transactor,
		publisher:   publisher,
		now:         time.Now,
		batchSize:   defaultPublisherBatchSize,
		maxAttempts: defaultPublisherMaxAttempts,
		retryBase:   defaultPublisherRetryBase,
	}
	for _, opt := range opts {
		opt(worker)
	}

	return worker
}

func (w *PublisherWorker) RunOnce(ctx context.Context) (PublishBatchResult, error) {
	startedAt := w.now()
	result := PublishBatchResult{}
	if w.transactor == nil {
		return result, errors.New("outbox transactor is required")
	}
	if w.publisher == nil {
		return result, errors.New("outbox publisher is required")
	}

	err := w.transactor.WithinTransaction(ctx, func(repo OutboxRepository) error {
		outbox := NewOutboxService(
			repo,
			WithNow(w.now),
			WithMaxAttempts(w.maxAttempts),
			WithRetryDelay(w.retryBase),
		)
		events, err := outbox.ListPendingReadyLocked(ctx, w.batchSize)
		if err != nil {
			return err
		}
		result.Fetched = len(events)

		for _, event := range events {
			if err := w.publisher.Publish(ctx, event); err != nil {
				result.Failed++
				willDeadLetter := event.Attempts+1 >= w.maxAttempts
				if recordErr := outbox.RecordPublishFailure(ctx, event); recordErr != nil {
					return recordErr
				}
				if willDeadLetter {
					result.DeadLettered++
				}
				logger.Error("outbox_publish_failed event_id="+event.ID+" event_type="+event.EventType+" aggregate_id="+event.AggregateID, err)
				continue
			}

			if err := outbox.MarkPublished(ctx, event.ID); err != nil {
				return err
			}
			result.Published++
			logger.Info("outbox_published event_id=", event.ID, " event_type=", event.EventType, " aggregate_id=", event.AggregateID)
		}

		return nil
	})
	result.Latency = w.now().Sub(startedAt)
	if err != nil {
		return result, err
	}

	return result, nil
}
