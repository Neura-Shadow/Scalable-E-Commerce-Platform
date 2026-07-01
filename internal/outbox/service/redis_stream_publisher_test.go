package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"

	"goshop/internal/outbox/model"
	redisMocks "goshop/pkg/redis/mocks"
)

func TestRedisStreamPublisherPublishesEventWithMetadataAndJSONPayload(t *testing.T) {
	createdAt := time.Date(2026, 7, 1, 10, 0, 0, 123, time.UTC)
	event := &model.OutboxEvent{
		ID:            "evt-1",
		AggregateType: "order",
		AggregateID:   "order-1",
		EventType:     "order.created",
		Payload:       datatypes.JSON([]byte(`{"order_id":"order-1","user_id":"user-1"}`)),
		CreatedAt:     createdAt,
	}
	cache := redisMocks.NewIRedis(t)
	cache.On("XAdd", mock.Anything, "stream:test-orders", mock.MatchedBy(func(values map[string]interface{}) bool {
		if values["event_id"] != event.ID {
			return false
		}
		if values["aggregate_type"] != event.AggregateType {
			return false
		}
		if values["aggregate_id"] != event.AggregateID {
			return false
		}
		if values["event_type"] != event.EventType {
			return false
		}
		if values["created_at"] != createdAt.Format(time.RFC3339Nano) {
			return false
		}
		payload, ok := values["payload"].(string)
		if !ok || !json.Valid([]byte(payload)) {
			return false
		}
		var decoded map[string]string
		if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
			return false
		}
		return decoded["order_id"] == "order-1" && decoded["user_id"] == "user-1"
	})).Return("1700000000000-0", nil).Once()

	publisher := NewRedisStreamPublisher(cache, "stream:test-orders")

	require.NoError(t, publisher.Publish(context.Background(), event))
}

func TestRedisStreamPublisherUsesDefaultStreamName(t *testing.T) {
	event := &model.OutboxEvent{
		ID:            "evt-1",
		AggregateType: "order",
		AggregateID:   "order-1",
		EventType:     "order.created",
		Payload:       datatypes.JSON([]byte(`{"order_id":"order-1"}`)),
		CreatedAt:     time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
	}
	cache := redisMocks.NewIRedis(t)
	cache.On("XAdd", mock.Anything, "stream:orders", mock.AnythingOfType("map[string]interface {}")).
		Return("1700000000000-0", nil).
		Once()

	publisher := NewRedisStreamPublisher(cache, "")

	require.NoError(t, publisher.Publish(context.Background(), event))
}

func TestRedisStreamPublisherReturnsXAddError(t *testing.T) {
	publishErr := errors.New("redis unavailable")
	event := &model.OutboxEvent{
		ID:            "evt-1",
		AggregateType: "order",
		AggregateID:   "order-1",
		EventType:     "order.created",
		Payload:       datatypes.JSON([]byte(`{"order_id":"order-1"}`)),
		CreatedAt:     time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
	}
	cache := redisMocks.NewIRedis(t)
	cache.On("XAdd", mock.Anything, "stream:orders", mock.AnythingOfType("map[string]interface {}")).
		Return("", publishErr).
		Once()

	publisher := NewRedisStreamPublisher(cache, "stream:orders")

	require.ErrorIs(t, publisher.Publish(context.Background(), event), publishErr)
}
