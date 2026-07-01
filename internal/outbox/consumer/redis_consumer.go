package consumer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/quangdangfit/gocommon/logger"

	"goshop/pkg/redis"
)

const (
	DefaultStreamName       = "stream:orders"
	DefaultGroupName        = "order-events"
	DefaultConsumerName     = "local-consumer-1"
	DefaultBatchSize        = int64(10)
	DefaultBlockDuration    = 5 * time.Second
	DefaultClaimMinIdle     = time.Minute
	DefaultClaimBatchSize   = int64(10)
	DefaultClaimStartID     = "0-0"
	consumerGroupStartID    = "0"
	redisBusyGroupErrMarker = "BUSYGROUP"
)

var ErrInvalidPayload = errors.New("stream event payload must be valid JSON")

type RedisConsumer struct {
	cache          redis.IRedis
	handler        EventHandler
	processedStore ProcessedEventStore
	streamName     string
	groupName      string
	consumerName   string
	batchSize      int64
	block          time.Duration
	claimMinIdle   time.Duration
	claimBatchSize int64
	claimStartID   string
}

type RedisConsumerOption func(*RedisConsumer)

type BatchResult struct {
	Read           int
	Processed      int
	Skipped        int
	Failed         int
	Invalid        int
	Acked          int
	NextClaimStart string
}

func WithStreamName(streamName string) RedisConsumerOption {
	return func(c *RedisConsumer) {
		if streamName != "" {
			c.streamName = streamName
		}
	}
}

func WithGroupName(groupName string) RedisConsumerOption {
	return func(c *RedisConsumer) {
		if groupName != "" {
			c.groupName = groupName
		}
	}
}

func WithConsumerName(consumerName string) RedisConsumerOption {
	return func(c *RedisConsumer) {
		if consumerName != "" {
			c.consumerName = consumerName
		}
	}
}

func WithBatchSize(batchSize int64) RedisConsumerOption {
	return func(c *RedisConsumer) {
		if batchSize > 0 {
			c.batchSize = batchSize
		}
	}
}

func WithBlock(block time.Duration) RedisConsumerOption {
	return func(c *RedisConsumer) {
		if block > 0 {
			c.block = block
		}
	}
}

func WithClaimMinIdle(minIdle time.Duration) RedisConsumerOption {
	return func(c *RedisConsumer) {
		if minIdle > 0 {
			c.claimMinIdle = minIdle
		}
	}
}

func WithClaimBatchSize(batchSize int64) RedisConsumerOption {
	return func(c *RedisConsumer) {
		if batchSize > 0 {
			c.claimBatchSize = batchSize
		}
	}
}

func NewRedisConsumer(cache redis.IRedis, handler EventHandler, processedStore ProcessedEventStore, opts ...RedisConsumerOption) *RedisConsumer {
	consumer := &RedisConsumer{
		cache:          cache,
		handler:        handler,
		processedStore: processedStore,
		streamName:     DefaultStreamName,
		groupName:      DefaultGroupName,
		consumerName:   DefaultConsumerName,
		batchSize:      DefaultBatchSize,
		block:          DefaultBlockDuration,
		claimMinIdle:   DefaultClaimMinIdle,
		claimBatchSize: DefaultClaimBatchSize,
		claimStartID:   DefaultClaimStartID,
	}
	for _, opt := range opts {
		opt(consumer)
	}
	return consumer
}

func (c *RedisConsumer) EnsureGroup(ctx context.Context) error {
	if c.cache == nil {
		return errors.New("redis cache is required")
	}
	if err := c.cache.XGroupCreateMkStream(ctx, c.streamName, c.groupName, consumerGroupStartID); err != nil {
		if strings.Contains(err.Error(), redisBusyGroupErrMarker) {
			return nil
		}
		return fmt.Errorf("create redis stream consumer group: %w", err)
	}
	return nil
}

func (c *RedisConsumer) RunOnce(ctx context.Context) (BatchResult, error) {
	if err := c.validate(); err != nil {
		return BatchResult{}, err
	}

	messages, err := c.cache.XReadGroup(ctx, c.groupName, c.consumerName, c.streamName, c.batchSize, c.block)
	if err != nil {
		return BatchResult{}, fmt.Errorf("read redis stream consumer group: %w", err)
	}
	return c.processMessages(ctx, messages)
}

func (c *RedisConsumer) ClaimStaleOnce(ctx context.Context) (BatchResult, error) {
	if err := c.validate(); err != nil {
		return BatchResult{}, err
	}

	messages, nextStart, err := c.cache.XAutoClaim(
		ctx,
		c.streamName,
		c.groupName,
		c.consumerName,
		c.claimStartID,
		c.claimMinIdle,
		c.claimBatchSize,
	)
	if err != nil {
		return BatchResult{}, fmt.Errorf("claim stale redis stream messages: %w", err)
	}
	result, err := c.processMessages(ctx, messages)
	result.NextClaimStart = nextStart
	if err != nil {
		return result, err
	}
	return result, nil
}

func (c *RedisConsumer) validate() error {
	if c.cache == nil {
		return errors.New("redis cache is required")
	}
	if c.handler == nil {
		return errors.New("event handler is required")
	}
	if c.processedStore == nil {
		return errors.New("processed event store is required")
	}
	return nil
}

func (c *RedisConsumer) processMessages(ctx context.Context, messages []redis.RedisStreamMessage) (BatchResult, error) {
	result := BatchResult{Read: len(messages)}
	for _, message := range messages {
		event, err := ParseStreamMessage(message)
		if err != nil {
			result.Invalid++
			result.Failed++
			logger.Error("outbox_consumer_invalid_message stream_message_id="+message.ID, err)
			continue
		}

		processed, err := c.processedStore.WasProcessed(ctx, event.EventID)
		if err != nil {
			result.Failed++
			logger.Error("outbox_consumer_idempotency_check_failed event_id="+event.EventID+" stream_message_id="+event.MessageID, err)
			continue
		}
		if processed {
			if err := c.ack(ctx, event); err != nil {
				result.Failed++
				logger.Error("outbox_consumer_ack_duplicate_failed event_id="+event.EventID+" stream_message_id="+event.MessageID, err)
				continue
			}
			result.Skipped++
			result.Acked++
			logger.Info("outbox_consumer_duplicate_skipped event_id=", event.EventID, " stream_message_id=", event.MessageID)
			continue
		}

		if err := c.handler.Handle(ctx, event); err != nil {
			result.Failed++
			logger.Error("outbox_consumer_handle_failed event_id="+event.EventID+" event_type="+event.EventType+" aggregate_id="+event.AggregateID, err)
			continue
		}

		if err := c.processedStore.MarkProcessed(ctx, event.EventID); err != nil {
			if errors.Is(err, ErrAlreadyProcessed) {
				if ackErr := c.ack(ctx, event); ackErr != nil {
					result.Failed++
					logger.Error("outbox_consumer_ack_concurrent_duplicate_failed event_id="+event.EventID+" stream_message_id="+event.MessageID, ackErr)
					continue
				}
				result.Skipped++
				result.Acked++
				continue
			}
			result.Failed++
			logger.Error("outbox_consumer_mark_processed_failed event_id="+event.EventID+" stream_message_id="+event.MessageID, err)
			continue
		}

		if err := c.ack(ctx, event); err != nil {
			result.Failed++
			logger.Error("outbox_consumer_ack_failed event_id="+event.EventID+" stream_message_id="+event.MessageID, err)
			continue
		}
		result.Processed++
		result.Acked++
	}
	return result, nil
}

func (c *RedisConsumer) ack(ctx context.Context, event StreamEvent) error {
	return c.cache.XAck(ctx, c.streamName, c.groupName, event.MessageID)
}

func requiredString(values map[string]interface{}, key string) (string, error) {
	value, ok := values[key]
	if !ok {
		return "", fmt.Errorf("stream event field %q is required", key)
	}
	switch v := value.(type) {
	case string:
		if v == "" {
			return "", fmt.Errorf("stream event field %q is required", key)
		}
		return v, nil
	case []byte:
		if len(v) == 0 {
			return "", fmt.Errorf("stream event field %q is required", key)
		}
		return string(v), nil
	default:
		text := fmt.Sprint(v)
		if text == "" {
			return "", fmt.Errorf("stream event field %q is required", key)
		}
		return text, nil
	}
}
