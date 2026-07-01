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
	handler := &recordingHandler{err: handlerErr}
	consumer := NewRedisConsumer(cache, handler, &fakeProcessedStore{processed: map[string]bool{}})

	result, err := consumer.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Read)
	assert.Equal(t, 1, result.Failed)
	assert.Equal(t, 0, result.Acked)
}

func TestRedisConsumerRunOnceSkipsDuplicateEventAndAcknowledges(t *testing.T) {
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

func TestRedisConsumerRunOnceHandlesInvalidMessageSafely(t *testing.T) {
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
				"created_at":     time.Now().UTC().Format(time.RFC3339Nano),
			},
		}}, nil).
		Once()
	handler := &recordingHandler{}
	consumer := NewRedisConsumer(cache, handler, &fakeProcessedStore{processed: map[string]bool{}})

	result, err := consumer.RunOnce(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, result.Read)
	assert.Equal(t, 1, result.Invalid)
	assert.Equal(t, 1, result.Failed)
	assert.Equal(t, 0, result.Acked)
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
