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
	DefaultStreamName           = "stream:orders"
	DefaultGroupName            = "order-events"
	DefaultConsumerName         = "local-consumer-1"
	DefaultBatchSize            = int64(10)
	DefaultBlockDuration        = 5 * time.Second
	DefaultClaimMinIdle         = time.Minute
	DefaultClaimBatchSize       = int64(10)
	DefaultClaimStartID         = "0-0"
	DefaultMaxDeliveryAttempts  = int64(5)
	DefaultFailureTTL           = 24 * time.Hour
	DefaultDeadLetterStreamName = "stream:orders:dead_letter"
	consumerGroupStartID        = "0"
	redisBusyGroupErrMarker     = "BUSYGROUP"
)

var ErrInvalidPayload = errors.New("stream event payload must be valid JSON")

type RedisConsumer struct {
	cache                redis.IRedis
	handler              EventHandler
	processedStore       ProcessedEventStore
	streamName           string
	groupName            string
	consumerName         string
	batchSize            int64
	block                time.Duration
	claimMinIdle         time.Duration
	claimBatchSize       int64
	claimStartID         string
	maxDeliveryAttempts  int64
	failureTTL           time.Duration
	deadLetterStreamName string
	now                  func() time.Time
}

type RedisConsumerOption func(*RedisConsumer)

type BatchResult struct {
	Read           int
	Processed      int
	Skipped        int
	Failed         int
	Invalid        int
	Acked          int
	DeadLettered   int
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

func WithMaxDeliveryAttempts(maxAttempts int64) RedisConsumerOption {
	return func(c *RedisConsumer) {
		if maxAttempts > 0 {
			c.maxDeliveryAttempts = maxAttempts
		}
	}
}

func WithFailureTTL(ttl time.Duration) RedisConsumerOption {
	return func(c *RedisConsumer) {
		if ttl > 0 {
			c.failureTTL = ttl
		}
	}
}

func WithDeadLetterStreamName(streamName string) RedisConsumerOption {
	return func(c *RedisConsumer) {
		if streamName != "" {
			c.deadLetterStreamName = streamName
		}
	}
}

func WithNow(now func() time.Time) RedisConsumerOption {
	return func(c *RedisConsumer) {
		if now != nil {
			c.now = now
		}
	}
}

func NewRedisConsumer(cache redis.IRedis, handler EventHandler, processedStore ProcessedEventStore, opts ...RedisConsumerOption) *RedisConsumer {
	consumer := &RedisConsumer{
		cache:                cache,
		handler:              handler,
		processedStore:       processedStore,
		streamName:           DefaultStreamName,
		groupName:            DefaultGroupName,
		consumerName:         DefaultConsumerName,
		batchSize:            DefaultBatchSize,
		block:                DefaultBlockDuration,
		claimMinIdle:         DefaultClaimMinIdle,
		claimBatchSize:       DefaultClaimBatchSize,
		claimStartID:         DefaultClaimStartID,
		maxDeliveryAttempts:  DefaultMaxDeliveryAttempts,
		failureTTL:           DefaultFailureTTL,
		deadLetterStreamName: DefaultDeadLetterStreamName,
		now:                  time.Now,
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
			if deadLetterErr := c.deadLetterInvalidMessage(ctx, message, err); deadLetterErr != nil {
				logger.Error("outbox_consumer_dead_letter_invalid_failed stream_message_id="+message.ID, deadLetterErr)
				continue
			}
			result.DeadLettered++
			if err := c.ackMessageID(ctx, message.ID); err != nil {
				logger.Error("outbox_consumer_ack_invalid_dead_letter_failed stream_message_id="+message.ID, err)
				continue
			}
			result.Acked++
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
			failureCount, recordErr := c.recordFailure(ctx, event)
			if recordErr != nil {
				logger.Error("outbox_consumer_failure_counter_failed event_id="+event.EventID+" stream_message_id="+event.MessageID, recordErr)
				continue
			}
			if failureCount >= c.maxDeliveryAttempts {
				if deadLetterErr := c.deadLetterEvent(ctx, event, failureCount, "handler_error", err); deadLetterErr != nil {
					logger.Error("outbox_consumer_dead_letter_failed event_id="+event.EventID+" stream_message_id="+event.MessageID, deadLetterErr)
					continue
				}
				result.DeadLettered++
				if err := c.ack(ctx, event); err != nil {
					logger.Error("outbox_consumer_ack_dead_letter_failed event_id="+event.EventID+" stream_message_id="+event.MessageID, err)
					continue
				}
				result.Acked++
				continue
			}
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
	return c.ackMessageID(ctx, event.MessageID)
}

func (c *RedisConsumer) ackMessageID(ctx context.Context, messageID string) error {
	return c.cache.XAck(ctx, c.streamName, c.groupName, messageID)
}

func (c *RedisConsumer) recordFailure(ctx context.Context, event StreamEvent) (int64, error) {
	return c.cache.IncrementWithExpiration(c.failureKey(event.EventID), c.failureTTL)
}

func (c *RedisConsumer) failureKey(eventID string) string {
	if eventID == "" {
		eventID = "unknown"
	}
	return fmt.Sprintf("consumer:failures:%s:%s:%s", c.streamName, c.groupName, eventID)
}

func (c *RedisConsumer) deadLetterEvent(ctx context.Context, event StreamEvent, failureCount int64, errorType string, _ error) error {
	values := map[string]interface{}{
		"original_stream":     c.streamName,
		"original_group":      c.groupName,
		"original_message_id": event.MessageID,
		"event_id":            event.EventID,
		"event_type":          event.EventType,
		"aggregate_type":      event.AggregateType,
		"aggregate_id":        event.AggregateID,
		"payload":             string(event.Payload),
		"failure_count":       failureCount,
		"error_type":          errorType,
		"dead_lettered_at":    c.now().UTC().Format(time.RFC3339Nano),
	}

	_, err := c.cache.XAdd(ctx, c.deadLetterStreamName, values)
	return err
}

func (c *RedisConsumer) deadLetterInvalidMessage(ctx context.Context, message redis.RedisStreamMessage, _ error) error {
	eventID := optionalString(message.Values, "event_id")
	if eventID == "" {
		eventID = message.ID
	}
	values := map[string]interface{}{
		"original_stream":     c.streamName,
		"original_group":      c.groupName,
		"original_message_id": message.ID,
		"event_id":            eventID,
		"event_type":          optionalString(message.Values, "event_type"),
		"aggregate_type":      optionalString(message.Values, "aggregate_type"),
		"aggregate_id":        optionalString(message.Values, "aggregate_id"),
		"payload":             optionalString(message.Values, "payload"),
		"failure_count":       int64(1),
		"error_type":          "parse_error",
		"dead_lettered_at":    c.now().UTC().Format(time.RFC3339Nano),
	}

	_, err := c.cache.XAdd(ctx, c.deadLetterStreamName, values)
	return err
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

func optionalString(values map[string]interface{}, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok {
		return ""
	}
	text, err := streamFieldValueToString(value)
	if err != nil {
		return ""
	}
	return text
}

func streamFieldValueToString(value interface{}) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	default:
		return fmt.Sprint(v), nil
	}
}
