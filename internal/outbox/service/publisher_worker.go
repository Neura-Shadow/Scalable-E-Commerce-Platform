package service

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/quangdangfit/gocommon/logger"

	"goshop/internal/outbox/model"
	appMetrics "goshop/pkg/metrics"
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
	transactor        OutboxTransactor
	publisher         EventPublisher
	now               func() time.Time
	workerID          string
	batchSize         int
	maxAttempts       int
	retryBase         time.Duration
	processingTimeout time.Duration
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

func WithPublisherWorkerID(workerID string) PublisherWorkerOption {
	return func(w *PublisherWorker) {
		if workerID != "" {
			w.workerID = workerID
		}
	}
}

func WithPublisherProcessingTimeout(timeout time.Duration) PublisherWorkerOption {
	return func(w *PublisherWorker) {
		if timeout > 0 {
			w.processingTimeout = timeout
		}
	}
}

func NewPublisherWorker(transactor OutboxTransactor, publisher EventPublisher, opts ...PublisherWorkerOption) *PublisherWorker {
	worker := &PublisherWorker{
		transactor:        transactor,
		publisher:         publisher,
		now:               time.Now,
		workerID:          defaultPublisherWorkerID(),
		batchSize:         defaultPublisherBatchSize,
		maxAttempts:       defaultPublisherMaxAttempts,
		retryBase:         defaultPublisherRetryBase,
		processingTimeout: 15 * time.Minute,
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

	var events []*model.OutboxEvent
	err := w.transactor.WithinTransaction(ctx, func(repo OutboxRepository) error {
		outbox := NewOutboxService(
			repo,
			WithNow(w.now),
			WithMaxAttempts(w.maxAttempts),
			WithRetryDelay(w.retryBase),
		)
		var err error
		events, err = outbox.ClaimReady(ctx, w.batchSize, w.workerID, w.processingTimeout)
		if err != nil {
			return err
		}
		result.Fetched = len(events)

		return nil
	})
	result.Latency = w.now().Sub(startedAt)
	if err != nil {
		appMetrics.RecordOutboxClaimFailure("claim_error")
		return result, err
	}

	for _, event := range events {
		appMetrics.RecordOutboxClaim(event.EventType)
		appMetrics.RecordOutboxPublishAttempt(event.EventType)
		eventStartedAt := w.now()
		if err := w.publisher.Publish(ctx, event); err != nil {
			publishDuration := w.now().Sub(eventStartedAt)
			appMetrics.RecordOutboxPublishFailure(event.EventType, "publish_error", publishDuration)
			result.Failed++
			willDeadLetter := event.Attempts+1 >= w.maxAttempts
			if recordErr := w.recordPublishFailure(ctx, event); recordErr != nil {
				appMetrics.RecordOutboxPublishFailure(event.EventType, "record_publish_failure", 0)
				appMetrics.RecordOutboxFinalizeFailure(event.EventType, "record_publish_failure")
				result.Latency = w.now().Sub(startedAt)
				return result, recordErr
			}
			if willDeadLetter {
				result.DeadLettered++
				appMetrics.RecordOutboxDeadLetter(event.EventType, "publish_exhausted")
			}
			logger.Error("outbox_publish_failed event_id="+event.ID+" event_type="+event.EventType+" aggregate_id="+event.AggregateID, err)
			continue
		}

		if err := w.markPublished(ctx, event.ID); err != nil {
			appMetrics.RecordOutboxPublishFailure(event.EventType, "mark_published_failed", w.now().Sub(eventStartedAt))
			appMetrics.RecordOutboxFinalizeFailure(event.EventType, "mark_published_failed")
			result.Latency = w.now().Sub(startedAt)
			return result, err
		}
		appMetrics.RecordOutboxPublishSuccess(event.EventType, w.now().Sub(eventStartedAt))
		result.Published++
		logger.Info("outbox_published event_id=", event.ID, " event_type=", event.EventType, " aggregate_id=", event.AggregateID)
	}

	result.Latency = w.now().Sub(startedAt)
	return result, nil
}

func (w *PublisherWorker) markPublished(ctx context.Context, eventID string) error {
	return w.transactor.WithinTransaction(ctx, func(repo OutboxRepository) error {
		outbox := NewOutboxService(
			repo,
			WithNow(w.now),
			WithMaxAttempts(w.maxAttempts),
			WithRetryDelay(w.retryBase),
		)
		return outbox.MarkPublished(ctx, eventID)
	})
}

func (w *PublisherWorker) recordPublishFailure(ctx context.Context, event *model.OutboxEvent) error {
	return w.transactor.WithinTransaction(ctx, func(repo OutboxRepository) error {
		outbox := NewOutboxService(
			repo,
			WithNow(w.now),
			WithMaxAttempts(w.maxAttempts),
			WithRetryDelay(w.retryBase),
		)
		return outbox.RecordPublishFailure(ctx, event)
	})
}

func defaultPublisherWorkerID() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return "publisher-worker"
	}
	return hostname
}
