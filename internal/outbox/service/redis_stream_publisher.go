package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"goshop/internal/outbox/model"
	"goshop/pkg/redis"
)

const DefaultRedisStreamName = "stream:orders"

type RedisStreamPublisher struct {
	cache      redis.IRedis
	streamName string
}

func NewRedisStreamPublisher(cache redis.IRedis, streamName string) *RedisStreamPublisher {
	if streamName == "" {
		streamName = DefaultRedisStreamName
	}

	return &RedisStreamPublisher{
		cache:      cache,
		streamName: streamName,
	}
}

func (p *RedisStreamPublisher) Publish(ctx context.Context, event *model.OutboxEvent) error {
	if p.cache == nil {
		return errors.New("redis cache is required")
	}
	if event == nil {
		return errors.New("outbox event is required")
	}

	payload := string(event.Payload)
	if payload == "" {
		payload = "null"
	}
	if !json.Valid([]byte(payload)) {
		return errors.New("outbox event payload must be valid JSON")
	}

	createdAt := event.CreatedAt.UTC().Format(time.RFC3339Nano)
	values := map[string]interface{}{
		"event_id":       event.ID,
		"aggregate_type": event.AggregateType,
		"aggregate_id":   event.AggregateID,
		"event_type":     event.EventType,
		"payload":        payload,
		"created_at":     createdAt,
	}

	if _, err := p.cache.XAdd(ctx, p.streamName, values); err != nil {
		return fmt.Errorf("publish outbox event to redis stream: %w", err)
	}

	return nil
}
