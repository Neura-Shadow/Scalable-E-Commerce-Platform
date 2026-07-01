package consumer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"goshop/pkg/redis"
)

var ErrAlreadyProcessed = errors.New("event already processed")

type ProcessedEventStore interface {
	WasProcessed(ctx context.Context, eventID string) (bool, error)
	MarkProcessed(ctx context.Context, eventID string) error
}

type RedisProcessedEventStore struct {
	cache redis.IRedis
	ttl   time.Duration
}

func NewRedisProcessedEventStore(cache redis.IRedis, ttl time.Duration) *RedisProcessedEventStore {
	return &RedisProcessedEventStore{
		cache: cache,
		ttl:   ttl,
	}
}

func (s *RedisProcessedEventStore) WasProcessed(ctx context.Context, eventID string) (bool, error) {
	if s.cache == nil {
		return false, errors.New("redis cache is required")
	}
	return s.cache.Exists(ctx, processedEventKey(eventID))
}

func (s *RedisProcessedEventStore) MarkProcessed(_ context.Context, eventID string) error {
	if s.cache == nil {
		return errors.New("redis cache is required")
	}
	ok, err := s.cache.SetNXWithExpiration(processedEventKey(eventID), true, s.ttl)
	if err != nil {
		return err
	}
	if !ok {
		return ErrAlreadyProcessed
	}
	return nil
}

func processedEventKey(eventID string) string {
	return fmt.Sprintf("processed:events:%s", eventID)
}
