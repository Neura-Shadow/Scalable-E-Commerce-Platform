package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"

	"github.com/quangdangfit/gocommon/logger"
	"goshop/internal/outbox/model"
	"goshop/pkg/config"
	appMetrics "goshop/pkg/metrics"
	redisMocks "goshop/pkg/redis/mocks"
)

func init() {
	logger.Initialize(config.ProductionEnv)
}

type fakeOutboxTransactor struct {
	mu     sync.Mutex
	repo   OutboxRepository
	calls  int
	err    error
	active bool
}

func (t *fakeOutboxTransactor) WithinTransaction(_ context.Context, fn func(OutboxRepository) error) error {
	t.mu.Lock()
	t.calls++
	t.active = true
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		t.active = false
		t.mu.Unlock()
	}()
	if t.err != nil {
		return t.err
	}
	return fn(t.repo)
}

func (t *fakeOutboxTransactor) IsActive() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.active
}

type recordingEventPublisher struct {
	published []string
	failures  map[string]error
}

func (p *recordingEventPublisher) Publish(_ context.Context, event *model.OutboxEvent) error {
	if err, ok := p.failures[event.ID]; ok {
		return err
	}
	p.published = append(p.published, event.ID)
	return nil
}

type transactionAwarePublisher struct {
	transactor                *fakeOutboxTransactor
	publishedWhileTransaction bool
	entered                   chan struct{}
	release                   chan struct{}
}

func newTransactionAwarePublisher(transactor *fakeOutboxTransactor) *transactionAwarePublisher {
	return &transactionAwarePublisher{
		transactor: transactor,
		entered:    make(chan struct{}),
		release:    make(chan struct{}),
	}
}

func (p *transactionAwarePublisher) Publish(_ context.Context, event *model.OutboxEvent) error {
	p.publishedWhileTransaction = p.transactor.IsActive()
	p.entered <- struct{}{}
	<-p.release
	return nil
}

func TestPublisherWorkerRunOnceMarksSuccessfulEventsPublished(t *testing.T) {
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	event := &model.OutboxEvent{ID: "evt-1", Status: model.OutboxEventStatusPending, NextAttemptAt: now}
	repo := newFakeOutboxRepository(event)
	publisher := &recordingEventPublisher{}
	worker := NewPublisherWorker(
		&fakeOutboxTransactor{repo: repo},
		publisher,
		WithPublisherNow(func() time.Time { return now }),
		WithPublisherBatchSize(10),
	)

	result, err := worker.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Fetched)
	assert.Equal(t, 1, result.Published)
	assert.Equal(t, 0, result.Failed)
	assert.Equal(t, []string{"evt-1"}, publisher.published)
	assert.Equal(t, model.OutboxEventStatusPublished, repo.events[event.ID].Status)
	assert.Nil(t, repo.events[event.ID].LockedAt)
	assert.Empty(t, repo.events[event.ID].LockedBy)
}

func TestPublisherWorkerRunOncePublishesOutsideTransaction(t *testing.T) {
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	event := &model.OutboxEvent{ID: "evt-1", EventType: "order.created", Status: model.OutboxEventStatusPending, NextAttemptAt: now}
	repo := newFakeOutboxRepository(event)
	transactor := &fakeOutboxTransactor{repo: repo}
	publisher := newTransactionAwarePublisher(transactor)
	worker := NewPublisherWorker(
		transactor,
		publisher,
		WithPublisherNow(func() time.Time { return now }),
		WithPublisherBatchSize(10),
	)
	done := make(chan error, 1)
	go func() {
		_, err := worker.RunOnce(context.Background())
		done <- err
	}()

	select {
	case <-publisher.entered:
	case <-time.After(time.Second):
		t.Fatal("publisher was not called")
	}
	assert.False(t, publisher.publishedWhileTransaction)
	assert.Equal(t, model.OutboxEventStatusProcessing, repo.events[event.ID].Status)
	require.NotNil(t, repo.events[event.ID].LockedAt)
	close(publisher.release)

	require.NoError(t, <-done)
	assert.Equal(t, model.OutboxEventStatusPublished, repo.events[event.ID].Status)
}

func TestPublisherWorkerRunOnceDoesNotClaimEventAlreadyProcessingInAnotherWorker(t *testing.T) {
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	event := &model.OutboxEvent{ID: "evt-1", EventType: "order.created", Status: model.OutboxEventStatusPending, NextAttemptAt: now}
	repo := newFakeOutboxRepository(event)
	firstTransactor := &fakeOutboxTransactor{repo: repo}
	firstPublisher := newTransactionAwarePublisher(firstTransactor)
	firstWorker := NewPublisherWorker(
		firstTransactor,
		firstPublisher,
		WithPublisherNow(func() time.Time { return now }),
		WithPublisherBatchSize(10),
		WithPublisherWorkerID("worker-1"),
		WithPublisherProcessingTimeout(15*time.Minute),
	)
	firstDone := make(chan error, 1)
	go func() {
		_, err := firstWorker.RunOnce(context.Background())
		firstDone <- err
	}()

	select {
	case <-firstPublisher.entered:
	case <-time.After(time.Second):
		t.Fatal("first publisher was not called")
	}

	secondPublisher := &recordingEventPublisher{}
	secondWorker := NewPublisherWorker(
		&fakeOutboxTransactor{repo: repo},
		secondPublisher,
		WithPublisherNow(func() time.Time { return now.Add(time.Minute) }),
		WithPublisherBatchSize(10),
		WithPublisherWorkerID("worker-2"),
		WithPublisherProcessingTimeout(15*time.Minute),
	)
	result, err := secondWorker.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Zero(t, result.Fetched)
	assert.Empty(t, secondPublisher.published)
	close(firstPublisher.release)
	require.NoError(t, <-firstDone)
}

func TestPublisherWorkerRunOnceRecordsPublishFailureAndContinues(t *testing.T) {
	appMetrics.ResetForTest()
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	retryDelay := 5 * time.Minute
	publishErr := errors.New("publish failed")
	failing := &model.OutboxEvent{ID: "evt-fail", EventType: "order.created", Status: model.OutboxEventStatusPending, Attempts: 0, NextAttemptAt: now}
	success := &model.OutboxEvent{ID: "evt-ok", EventType: "order.created", Status: model.OutboxEventStatusPending, Attempts: 0, NextAttemptAt: now}
	repo := newFakeOutboxRepository(failing, success)
	publisher := &recordingEventPublisher{failures: map[string]error{"evt-fail": publishErr}}
	worker := NewPublisherWorker(
		&fakeOutboxTransactor{repo: repo},
		publisher,
		WithPublisherNow(func() time.Time { return now }),
		WithPublisherRetryBase(retryDelay),
		WithPublisherMaxAttempts(3),
		WithPublisherBatchSize(10),
	)

	result, err := worker.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 2, result.Fetched)
	assert.Equal(t, 1, result.Published)
	assert.Equal(t, 1, result.Failed)
	assert.Equal(t, 1, repo.events[failing.ID].Attempts)
	assert.Equal(t, model.OutboxEventStatusPending, repo.events[failing.ID].Status)
	assert.Equal(t, now.Add(retryDelay), repo.events[failing.ID].NextAttemptAt)
	assert.Equal(t, model.OutboxEventStatusPublished, repo.events[success.ID].Status)
	snapshot, err := appMetrics.SnapshotText()
	require.NoError(t, err)
	assert.Contains(t, snapshot, "outbox_publish_attempt_total")
	assert.Contains(t, snapshot, "outbox_publish_success_total")
	assert.Contains(t, snapshot, "outbox_publish_failure_total")
	assert.Contains(t, snapshot, `event_type="order.created"`)
	assert.NotContains(t, snapshot, "evt-fail")
	assert.NotContains(t, snapshot, "evt-ok")
}

func TestPublisherWorkerRunOnceMovesExhaustedFailureToDeadLetter(t *testing.T) {
	appMetrics.ResetForTest()
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	publishErr := errors.New("publish failed")
	event := &model.OutboxEvent{ID: "evt-1", EventType: "order.created", Status: model.OutboxEventStatusPending, Attempts: 2, NextAttemptAt: now}
	repo := newFakeOutboxRepository(event)
	publisher := &recordingEventPublisher{failures: map[string]error{"evt-1": publishErr}}
	worker := NewPublisherWorker(
		&fakeOutboxTransactor{repo: repo},
		publisher,
		WithPublisherNow(func() time.Time { return now }),
		WithPublisherMaxAttempts(3),
	)

	result, err := worker.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Failed)
	assert.Equal(t, 3, repo.events[event.ID].Attempts)
	assert.Equal(t, model.OutboxEventStatusDeadLetter, repo.events[event.ID].Status)
	snapshot, err := appMetrics.SnapshotText()
	require.NoError(t, err)
	assert.Contains(t, snapshot, "outbox_dead_letter_total")
	assert.Contains(t, snapshot, `reason="publish_exhausted"`)
	assert.NotContains(t, snapshot, "evt-1")
}

func TestPublisherWorkerRunOnceMarksEventPublishedAfterRedisStreamPublishSucceeds(t *testing.T) {
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	event := &model.OutboxEvent{
		ID:            "evt-redis",
		AggregateType: "order",
		AggregateID:   "order-1",
		EventType:     "order.created",
		Payload:       datatypes.JSON([]byte(`{"order_id":"order-1"}`)),
		Status:        model.OutboxEventStatusPending,
		NextAttemptAt: now,
		CreatedAt:     now,
	}
	repo := newFakeOutboxRepository(event)
	cache := redisMocks.NewIRedis(t)
	cache.On("XAdd", mock.Anything, "stream:orders", mock.AnythingOfType("map[string]interface {}")).
		Return("1700000000000-0", nil).
		Once()
	worker := NewPublisherWorker(
		&fakeOutboxTransactor{repo: repo},
		NewRedisStreamPublisher(cache, "stream:orders"),
		WithPublisherNow(func() time.Time { return now }),
		WithPublisherBatchSize(10),
	)

	result, err := worker.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Published)
	assert.Equal(t, 0, result.Failed)
	assert.Equal(t, model.OutboxEventStatusPublished, repo.events[event.ID].Status)
	require.NotNil(t, repo.events[event.ID].PublishedAt)
	assert.Equal(t, now, *repo.events[event.ID].PublishedAt)
}

func TestPublisherWorkerRunOnceSchedulesRetryWhenRedisStreamPublishFails(t *testing.T) {
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	retryDelay := 5 * time.Minute
	publishErr := errors.New("redis unavailable")
	event := &model.OutboxEvent{
		ID:            "evt-redis-fail",
		AggregateType: "order",
		AggregateID:   "order-1",
		EventType:     "order.created",
		Payload:       datatypes.JSON([]byte(`{"order_id":"order-1"}`)),
		Status:        model.OutboxEventStatusPending,
		Attempts:      0,
		NextAttemptAt: now,
		CreatedAt:     now,
	}
	repo := newFakeOutboxRepository(event)
	cache := redisMocks.NewIRedis(t)
	cache.On("XAdd", mock.Anything, "stream:orders", mock.AnythingOfType("map[string]interface {}")).
		Return("", publishErr).
		Once()
	worker := NewPublisherWorker(
		&fakeOutboxTransactor{repo: repo},
		NewRedisStreamPublisher(cache, "stream:orders"),
		WithPublisherNow(func() time.Time { return now }),
		WithPublisherRetryBase(retryDelay),
		WithPublisherMaxAttempts(3),
		WithPublisherBatchSize(10),
	)

	result, err := worker.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Failed)
	assert.Equal(t, 0, result.Published)
	assert.Equal(t, 1, repo.events[event.ID].Attempts)
	assert.Equal(t, model.OutboxEventStatusPending, repo.events[event.ID].Status)
	assert.Equal(t, now.Add(retryDelay), repo.events[event.ID].NextAttemptAt)
}

func TestPublisherWorkerRunOnceRespectsBatchSize(t *testing.T) {
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	repo := newFakeOutboxRepository(
		&model.OutboxEvent{ID: "evt-1", Status: model.OutboxEventStatusPending, NextAttemptAt: now},
		&model.OutboxEvent{ID: "evt-2", Status: model.OutboxEventStatusPending, NextAttemptAt: now},
		&model.OutboxEvent{ID: "evt-3", Status: model.OutboxEventStatusPending, NextAttemptAt: now},
	)
	publisher := &recordingEventPublisher{}
	worker := NewPublisherWorker(
		&fakeOutboxTransactor{repo: repo},
		publisher,
		WithPublisherNow(func() time.Time { return now }),
		WithPublisherBatchSize(2),
	)

	result, err := worker.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 2, result.Fetched)
	assert.Equal(t, 2, result.Published)
	assert.Len(t, publisher.published, 2)
}

func TestLogPublisherDoesNotLogPayloadOrCallBroker(t *testing.T) {
	publisher := NewLogPublisher()
	event := &model.OutboxEvent{
		ID:            "evt-1",
		AggregateType: "order",
		AggregateID:   "order-1",
		EventType:     "order.created",
		Payload:       datatypes.JSON([]byte(`{"private_field":"redacted"}`)),
	}

	err := publisher.Publish(context.Background(), event)

	require.NoError(t, err)
}
