package consumer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/quangdangfit/gocommon/logger"
	"goshop/pkg/config"
	appMetrics "goshop/pkg/metrics"
	"goshop/pkg/redis"
	redisMocks "goshop/pkg/redis/mocks"
)

func init() {
	logger.Initialize(config.ProductionEnv)
}

type recordingHandler struct {
	events []StreamEvent
	err    error
}

func (h *recordingHandler) Handle(_ context.Context, event StreamEvent) error {
	if h.err != nil {
		return h.err
	}
	h.events = append(h.events, event)
	return nil
}

type fakeProcessedStore struct {
	processed map[string]bool
	marked    []string
	err       error
}

func (s *fakeProcessedStore) WasProcessed(_ context.Context, eventID string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.processed[eventID], nil
}

func (s *fakeProcessedStore) MarkProcessed(_ context.Context, eventID string) error {
	if s.err != nil {
		return s.err
	}
	s.marked = append(s.marked, eventID)
	if s.processed == nil {
		s.processed = map[string]bool{}
	}
	s.processed[eventID] = true
	return nil
}

func TestRedisConsumerEnsureGroupCreatesConsumerGroup(t *testing.T) {
	cache := redisMocks.NewIRedis(t)
	cache.On("XGroupCreateMkStream", mock.Anything, "stream:orders", "order-events", "0").
		Return(nil).
		Once()
	consumer := NewRedisConsumer(cache, &recordingHandler{}, &fakeProcessedStore{})

	require.NoError(t, consumer.EnsureGroup(context.Background()))
}

func TestRedisConsumerRunOnceReadsParsesHandlesAndAcknowledges(t *testing.T) {
	createdAt := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	cache := redisMocks.NewIRedis(t)
	cache.On("XReadGroup", mock.Anything, "group-a", "consumer-a", "stream:a", int64(2), 5*time.Second).
		Return([]redis.RedisStreamMessage{validRedisMessage("redis-id-1", "evt-1", createdAt)}, nil).
		Once()
	cache.On("XAck", mock.Anything, "stream:a", "group-a", "redis-id-1").
		Return(nil).
		Once()
	handler := &recordingHandler{}
	store := &fakeProcessedStore{processed: map[string]bool{}}
	consumer := NewRedisConsumer(
		cache,
		handler,
		store,
		WithStreamName("stream:a"),
		WithGroupName("group-a"),
		WithConsumerName("consumer-a"),
		WithBatchSize(2),
		WithBlock(5*time.Second),
	)

	result, err := consumer.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Read)
	assert.Equal(t, 1, result.Processed)
	assert.Equal(t, 1, result.Acked)
	require.Len(t, handler.events, 1)
	assert.Equal(t, "evt-1", handler.events[0].EventID)
	assert.Equal(t, "order", handler.events[0].AggregateType)
	assert.Equal(t, "order-1", handler.events[0].AggregateID)
	assert.Equal(t, "order.created", handler.events[0].EventType)
	assert.JSONEq(t, `{"order_id":"order-1"}`, string(handler.events[0].Payload))
	assert.Equal(t, []string{"evt-1"}, store.marked)
}

func TestParseStreamMessageParsesValidEvent(t *testing.T) {
	createdAt := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)

	event, err := ParseStreamMessage(validRedisMessage("redis-id-1", "evt-1", createdAt))

	require.NoError(t, err)
	assert.Equal(t, "redis-id-1", event.MessageID)
	assert.Equal(t, "evt-1", event.EventID)
	assert.Equal(t, "order", event.AggregateType)
	assert.Equal(t, "order-1", event.AggregateID)
	assert.Equal(t, "order.created", event.EventType)
	assert.Equal(t, createdAt, event.CreatedAt)
	assert.JSONEq(t, `{"order_id":"order-1"}`, string(event.Payload))
}

func TestRedisConsumerRunOnceDoesNotAckWhenHandlerFails(t *testing.T) {
	handlerErr := errors.New("handler failed")
	cache := redisMocks.NewIRedis(t)
	cache.On("XReadGroup", mock.Anything, "order-events", "local-consumer-1", "stream:orders", int64(10), 5*time.Second).
		Return([]redis.RedisStreamMessage{validRedisMessage("redis-id-1", "evt-1", time.Now().UTC())}, nil).
		Once()
	cache.On("IncrementWithExpiration", "consumer:failures:stream:orders:order-events:evt-1", 24*time.Hour).
		Return(int64(1), nil).
		Once()
	handler := &recordingHandler{err: handlerErr}
	consumer := NewRedisConsumer(
		cache,
		handler,
		&fakeProcessedStore{processed: map[string]bool{}},
		WithMaxDeliveryAttempts(5),
		WithFailureTTL(24*time.Hour),
	)

	result, err := consumer.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Read)
	assert.Equal(t, 1, result.Failed)
	assert.Equal(t, 0, result.Acked)
	assert.Equal(t, 0, result.DeadLettered)
}

func TestRedisConsumerRunOnceDeadLettersHandlerFailureAtMaxAttempts(t *testing.T) {
	appMetrics.ResetForTest()
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	handlerErr := errors.New("handler failed")
	message := validRedisMessage("redis-id-1", "evt-1", now)
	cache := redisMocks.NewIRedis(t)
	cache.On("XReadGroup", mock.Anything, "order-events", "local-consumer-1", "stream:orders", int64(10), 5*time.Second).
		Return([]redis.RedisStreamMessage{message}, nil).
		Once()
	cache.On("IncrementWithExpiration", "consumer:failures:stream:orders:order-events:evt-1", 24*time.Hour).
		Return(int64(5), nil).
		Once()
	deadLetterCall := cache.On("XAdd", mock.Anything, "stream:orders:dead_letter", mock.MatchedBy(func(values map[string]interface{}) bool {
		return values["original_stream"] == "stream:orders" &&
			values["original_group"] == "order-events" &&
			values["original_message_id"] == "redis-id-1" &&
			values["event_id"] == "evt-1" &&
			values["event_type"] == "order.created" &&
			values["aggregate_type"] == "order" &&
			values["aggregate_id"] == "order-1" &&
			values["payload"] == `{"order_id":"order-1"}` &&
			values["failure_count"] == int64(5) &&
			values["error_type"] == "handler_error" &&
			values["dead_lettered_at"] == now.Format(time.RFC3339Nano)
	})).
		Return("dead-letter-id-1", nil).
		Once()
	cache.On("XAck", mock.Anything, "stream:orders", "order-events", "redis-id-1").
		Return(nil).
		Once().
		NotBefore(deadLetterCall)
	handler := &recordingHandler{err: handlerErr}
	consumer := NewRedisConsumer(
		cache,
		handler,
		&fakeProcessedStore{processed: map[string]bool{}},
		WithMaxDeliveryAttempts(5),
		WithFailureTTL(24*time.Hour),
		WithDeadLetterStreamName("stream:orders:dead_letter"),
		WithNow(func() time.Time { return now }),
	)

	result, err := consumer.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Read)
	assert.Equal(t, 1, result.Failed)
	assert.Equal(t, 1, result.DeadLettered)
	assert.Equal(t, 1, result.Acked)
	snapshot, err := appMetrics.SnapshotText()
	require.NoError(t, err)
	assert.Contains(t, snapshot, "outbox_consumer_dead_letter_total")
	assert.Contains(t, snapshot, `reason="handler_error"`)
	assert.Contains(t, snapshot, `result="success"`)
	assert.NotContains(t, snapshot, "evt-1")
	assert.NotContains(t, snapshot, "redis-id-1")
}

func TestRedisConsumerRunOnceDoesNotAckWhenDeadLetterWriteFails(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	handlerErr := errors.New("handler failed")
	deadLetterErr := errors.New("dead letter unavailable")
	cache := redisMocks.NewIRedis(t)
	cache.On("XReadGroup", mock.Anything, "order-events", "local-consumer-1", "stream:orders", int64(10), 5*time.Second).
		Return([]redis.RedisStreamMessage{validRedisMessage("redis-id-1", "evt-1", now)}, nil).
		Once()
	cache.On("IncrementWithExpiration", "consumer:failures:stream:orders:order-events:evt-1", 24*time.Hour).
		Return(int64(5), nil).
		Once()
	cache.On("XAdd", mock.Anything, "stream:orders:dead_letter", mock.AnythingOfType("map[string]interface {}")).
		Return("", deadLetterErr).
		Once()
	handler := &recordingHandler{err: handlerErr}
	consumer := NewRedisConsumer(
		cache,
		handler,
		&fakeProcessedStore{processed: map[string]bool{}},
		WithMaxDeliveryAttempts(5),
		WithFailureTTL(24*time.Hour),
		WithNow(func() time.Time { return now }),
	)

	result, err := consumer.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Read)
	assert.Equal(t, 1, result.Failed)
	assert.Equal(t, 0, result.DeadLettered)
	assert.Equal(t, 0, result.Acked)
}

func TestRedisConsumerRunOnceSkipsDuplicateEventAndAcknowledges(t *testing.T) {
	appMetrics.ResetForTest()
	cache := redisMocks.NewIRedis(t)
	cache.On("XReadGroup", mock.Anything, "order-events", "local-consumer-1", "stream:orders", int64(10), 5*time.Second).
		Return([]redis.RedisStreamMessage{validRedisMessage("redis-id-1", "evt-1", time.Now().UTC())}, nil).
		Once()
	cache.On("XAck", mock.Anything, "stream:orders", "order-events", "redis-id-1").
		Return(nil).
		Once()
	handler := &recordingHandler{}
	store := &fakeProcessedStore{processed: map[string]bool{"evt-1": true}}
	consumer := NewRedisConsumer(cache, handler, store)

	result, err := consumer.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Skipped)
	assert.Equal(t, 1, result.Acked)
	assert.Empty(t, handler.events)
	assert.Empty(t, store.marked)
	snapshot, err := appMetrics.SnapshotText()
	require.NoError(t, err)
	assert.Contains(t, snapshot, "outbox_consumer_duplicate_skipped_total")
	assert.Contains(t, snapshot, "outbox_consumer_ack_total")
	assert.Contains(t, snapshot, `event_type="order.created"`)
	assert.NotContains(t, snapshot, "evt-1")
	assert.NotContains(t, snapshot, "redis-id-1")
}

func TestRedisProcessedEventStoreMarksAndChecksProcessedEvent(t *testing.T) {
	ttl := time.Hour
	cache := redisMocks.NewIRedis(t)
	cache.On("SetNXWithExpiration", "processed:events:evt-1", true, ttl).
		Return(true, nil).
		Once()
	cache.On("Exists", mock.Anything, "processed:events:evt-1").
		Return(true, nil).
		Once()
	store := NewRedisProcessedEventStore(cache, ttl)

	require.NoError(t, store.MarkProcessed(context.Background(), "evt-1"))
	processed, err := store.WasProcessed(context.Background(), "evt-1")

	require.NoError(t, err)
	assert.True(t, processed)
}

func TestRedisConsumerClaimStaleOnceClaimsProcessesAndAcknowledges(t *testing.T) {
	createdAt := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	cache := redisMocks.NewIRedis(t)
	cache.On("XAutoClaim", mock.Anything, "stream:orders", "order-events", "local-consumer-1", "0-0", time.Minute, int64(2)).
		Return([]redis.RedisStreamMessage{validRedisMessage("redis-id-1", "evt-1", createdAt)}, "0-0", nil).
		Once()
	cache.On("XAck", mock.Anything, "stream:orders", "order-events", "redis-id-1").
		Return(nil).
		Once()
	handler := &recordingHandler{}
	store := &fakeProcessedStore{processed: map[string]bool{}}
	consumer := NewRedisConsumer(
		cache,
		handler,
		store,
		WithClaimMinIdle(time.Minute),
		WithClaimBatchSize(2),
	)

	result, err := consumer.ClaimStaleOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Read)
	assert.Equal(t, 1, result.Processed)
	assert.Equal(t, 1, result.Acked)
	assert.Equal(t, "0-0", result.NextClaimStart)
}

func TestRedisConsumerClaimStaleOnceHandlerFailureUsesDeadLetterLogic(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	cache := redisMocks.NewIRedis(t)
	cache.On("XAutoClaim", mock.Anything, "stream:orders", "order-events", "local-consumer-1", "0-0", time.Minute, int64(2)).
		Return([]redis.RedisStreamMessage{validRedisMessage("redis-id-1", "evt-1", now)}, "0-0", nil).
		Once()
	cache.On("IncrementWithExpiration", "consumer:failures:stream:orders:order-events:evt-1", 24*time.Hour).
		Return(int64(5), nil).
		Once()
	deadLetterCall := cache.On("XAdd", mock.Anything, "stream:orders:dead_letter", mock.AnythingOfType("map[string]interface {}")).
		Return("dead-letter-id-1", nil).
		Once()
	cache.On("XAck", mock.Anything, "stream:orders", "order-events", "redis-id-1").
		Return(nil).
		Once().
		NotBefore(deadLetterCall)
	handler := &recordingHandler{err: errors.New("handler failed")}
	consumer := NewRedisConsumer(
		cache,
		handler,
		&fakeProcessedStore{processed: map[string]bool{}},
		WithClaimMinIdle(time.Minute),
		WithClaimBatchSize(2),
		WithMaxDeliveryAttempts(5),
		WithFailureTTL(24*time.Hour),
		WithNow(func() time.Time { return now }),
	)

	result, err := consumer.ClaimStaleOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Read)
	assert.Equal(t, 1, result.Failed)
	assert.Equal(t, 1, result.DeadLettered)
	assert.Equal(t, 1, result.Acked)
}

func TestRedisConsumerRunOnceDeadLettersInvalidPayloadAndAcknowledges(t *testing.T) {
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	cache := redisMocks.NewIRedis(t)
	cache.On("XReadGroup", mock.Anything, "order-events", "local-consumer-1", "stream:orders", int64(10), 5*time.Second).
		Return([]redis.RedisStreamMessage{{
			ID: "redis-id-1",
			Values: map[string]interface{}{
				"event_id":       "evt-1",
				"aggregate_type": "order",
				"aggregate_id":   "order-1",
				"event_type":     "order.created",
				"payload":        `{"order_id":`,
				"created_at":     now.Format(time.RFC3339Nano),
			},
		}}, nil).
		Once()
	deadLetterCall := cache.On("XAdd", mock.Anything, "stream:orders:dead_letter", mock.MatchedBy(func(values map[string]interface{}) bool {
		return values["original_stream"] == "stream:orders" &&
			values["original_group"] == "order-events" &&
			values["original_message_id"] == "redis-id-1" &&
			values["event_id"] == "evt-1" &&
			values["event_type"] == "order.created" &&
			values["aggregate_type"] == "order" &&
			values["aggregate_id"] == "order-1" &&
			values["payload"] == `{"order_id":` &&
			values["failure_count"] == int64(1) &&
			values["error_type"] == "parse_error" &&
			values["dead_lettered_at"] == now.Format(time.RFC3339Nano)
	})).
		Return("dead-letter-id-1", nil).
		Once()
	cache.On("XAck", mock.Anything, "stream:orders", "order-events", "redis-id-1").
		Return(nil).
		Once().
		NotBefore(deadLetterCall)
	handler := &recordingHandler{}
	consumer := NewRedisConsumer(
		cache,
		handler,
		&fakeProcessedStore{processed: map[string]bool{}},
		WithNow(func() time.Time { return now }),
	)

	result, err := consumer.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Read)
	assert.Equal(t, 1, result.Invalid)
	assert.Equal(t, 1, result.Failed)
	assert.Equal(t, 1, result.DeadLettered)
	assert.Equal(t, 1, result.Acked)
	assert.Empty(t, handler.events)
}

func validRedisMessage(messageID, eventID string, createdAt time.Time) redis.RedisStreamMessage {
	return redis.RedisStreamMessage{
		ID: messageID,
		Values: map[string]interface{}{
			"event_id":       eventID,
			"aggregate_type": "order",
			"aggregate_id":   "order-1",
			"event_type":     "order.created",
			"payload":        `{"order_id":"order-1"}`,
			"created_at":     createdAt.Format(time.RFC3339Nano),
		},
	}
}
