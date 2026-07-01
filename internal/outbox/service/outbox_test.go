package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"goshop/internal/outbox/model"
)

type fakeOutboxRepository struct {
	mu      sync.Mutex
	events  map[string]*model.OutboxEvent
	created []*model.OutboxEvent
}

func newFakeOutboxRepository(events ...*model.OutboxEvent) *fakeOutboxRepository {
	repo := &fakeOutboxRepository{
		events: make(map[string]*model.OutboxEvent, len(events)),
	}
	for _, event := range events {
		repo.events[event.ID] = event
	}
	return repo
}

func (r *fakeOutboxRepository) CreatePending(_ context.Context, event *model.OutboxEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.created = append(r.created, event)
	if event.ID != "" {
		r.events[event.ID] = event
	}
	return nil
}

func (r *fakeOutboxRepository) ListPendingReady(_ context.Context, now time.Time, limit int) ([]*model.OutboxEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	ready := make([]*model.OutboxEvent, 0)
	for _, event := range r.events {
		if event.Status == model.OutboxEventStatusPending && !event.NextAttemptAt.After(now) {
			ready = append(ready, event)
		}
	}
	if limit > 0 && len(ready) > limit {
		ready = ready[:limit]
	}
	return ready, nil
}

func (r *fakeOutboxRepository) ListPendingReadyLocked(ctx context.Context, now time.Time, limit int) ([]*model.OutboxEvent, error) {
	return r.ListPendingReady(ctx, now, limit)
}

func (r *fakeOutboxRepository) MarkPublished(_ context.Context, eventID string, publishedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	event := r.events[eventID]
	event.Status = model.OutboxEventStatusPublished
	event.PublishedAt = &publishedAt
	return nil
}

func (r *fakeOutboxRepository) MarkPublishFailed(_ context.Context, eventID string, nextAttemptAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	event := r.events[eventID]
	event.Attempts++
	event.NextAttemptAt = nextAttemptAt
	event.Status = model.OutboxEventStatusPending
	return nil
}

func (r *fakeOutboxRepository) MarkDeadLetter(_ context.Context, eventID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events[eventID].Status = model.OutboxEventStatusDeadLetter
	return nil
}

type fakePublisher struct {
	err error
}

func (p fakePublisher) Publish(_ context.Context, _ *model.OutboxEvent) error {
	return p.err
}

func TestCreatePendingStoresJSONPayload(t *testing.T) {
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	repo := newFakeOutboxRepository()
	svc := NewOutboxService(repo, WithNow(func() time.Time { return now }))

	event, err := svc.CreatePending(context.Background(), "order", "order-1", "order.created", map[string]any{
		"order_id": "order-1",
		"user_id":  "user-1",
	})

	require.NoError(t, err)
	require.Len(t, repo.created, 1)
	assert.Same(t, repo.created[0], event)
	assert.Equal(t, "order", event.AggregateType)
	assert.Equal(t, "order-1", event.AggregateID)
	assert.Equal(t, "order.created", event.EventType)
	assert.Equal(t, model.OutboxEventStatusPending, event.Status)
	assert.Equal(t, 0, event.Attempts)
	assert.Equal(t, now, event.NextAttemptAt)

	var payload map[string]string
	require.NoError(t, json.Unmarshal(event.Payload, &payload))
	assert.Equal(t, "order-1", payload["order_id"])
	assert.Equal(t, "user-1", payload["user_id"])
}

func TestListPendingReadyReturnsReadyPendingEvents(t *testing.T) {
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	ready := &model.OutboxEvent{ID: "ready", Status: model.OutboxEventStatusPending, NextAttemptAt: now.Add(-time.Second)}
	future := &model.OutboxEvent{ID: "future", Status: model.OutboxEventStatusPending, NextAttemptAt: now.Add(time.Minute)}
	published := &model.OutboxEvent{ID: "published", Status: model.OutboxEventStatusPublished, NextAttemptAt: now.Add(-time.Second)}
	deadLetter := &model.OutboxEvent{ID: "dead-letter", Status: model.OutboxEventStatusDeadLetter, NextAttemptAt: now.Add(-time.Second)}
	repo := newFakeOutboxRepository(ready, future, published, deadLetter)
	svc := NewOutboxService(repo, WithNow(func() time.Time { return now }))

	events, err := svc.ListPendingReady(context.Background(), 10)

	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "ready", events[0].ID)
}

func TestListPendingReadyRespectsBatchSize(t *testing.T) {
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	repo := newFakeOutboxRepository(
		&model.OutboxEvent{ID: "ready-1", Status: model.OutboxEventStatusPending, NextAttemptAt: now},
		&model.OutboxEvent{ID: "ready-2", Status: model.OutboxEventStatusPending, NextAttemptAt: now},
		&model.OutboxEvent{ID: "ready-3", Status: model.OutboxEventStatusPending, NextAttemptAt: now},
	)
	svc := NewOutboxService(repo, WithNow(func() time.Time { return now }))

	events, err := svc.ListPendingReady(context.Background(), 2)

	require.NoError(t, err)
	assert.Len(t, events, 2)
}

func TestPublishMarksEventPublished(t *testing.T) {
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	event := &model.OutboxEvent{ID: "evt-1", Status: model.OutboxEventStatusPending, NextAttemptAt: now}
	repo := newFakeOutboxRepository(event)
	svc := NewOutboxService(repo, WithNow(func() time.Time { return now }))

	err := svc.Publish(context.Background(), fakePublisher{}, event)

	require.NoError(t, err)
	assert.Equal(t, model.OutboxEventStatusPublished, repo.events[event.ID].Status)
	require.NotNil(t, repo.events[event.ID].PublishedAt)
	assert.Equal(t, now, *repo.events[event.ID].PublishedAt)
}

func TestPublishFailureIncrementsAttemptsAndSchedulesRetry(t *testing.T) {
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	retryDelay := 5 * time.Minute
	publishErr := errors.New("publish failed")
	event := &model.OutboxEvent{ID: "evt-1", Status: model.OutboxEventStatusPending, Attempts: 0, NextAttemptAt: now}
	repo := newFakeOutboxRepository(event)
	svc := NewOutboxService(
		repo,
		WithNow(func() time.Time { return now }),
		WithRetryDelay(retryDelay),
		WithMaxAttempts(3),
	)

	err := svc.Publish(context.Background(), fakePublisher{err: publishErr}, event)

	require.ErrorIs(t, err, publishErr)
	assert.Equal(t, 1, repo.events[event.ID].Attempts)
	assert.Equal(t, model.OutboxEventStatusPending, repo.events[event.ID].Status)
	assert.Equal(t, now.Add(retryDelay), repo.events[event.ID].NextAttemptAt)
}

func TestExhaustedPublishFailureMovesToDeadLetter(t *testing.T) {
	now := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	publishErr := errors.New("publish failed")
	event := &model.OutboxEvent{ID: "evt-1", Status: model.OutboxEventStatusPending, Attempts: 2, NextAttemptAt: now}
	repo := newFakeOutboxRepository(event)
	svc := NewOutboxService(
		repo,
		WithNow(func() time.Time { return now }),
		WithRetryDelay(time.Minute),
		WithMaxAttempts(3),
	)

	err := svc.Publish(context.Background(), fakePublisher{err: publishErr}, event)

	require.ErrorIs(t, err, publishErr)
	assert.Equal(t, 3, repo.events[event.ID].Attempts)
	assert.Equal(t, model.OutboxEventStatusDeadLetter, repo.events[event.ID].Status)
}
