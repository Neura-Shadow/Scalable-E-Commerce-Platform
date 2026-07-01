package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOutboxPublisherConfigDefaults(t *testing.T) {
	previousPublisherType := cfg.OutboxPublisherType
	previousStreamName := cfg.OutboxRedisStreamName
	cfg.OutboxPublisherType = ""
	cfg.OutboxRedisStreamName = ""
	t.Cleanup(func() {
		cfg.OutboxPublisherType = previousPublisherType
		cfg.OutboxRedisStreamName = previousStreamName
	})

	assert.Equal(t, OutboxPublisherTypeLog, OutboxPublisherType())
	assert.Equal(t, DefaultOutboxRedisStreamName, OutboxRedisStreamName())
}

func TestOutboxConsumerConfigDefaults(t *testing.T) {
	previousEnabled := cfg.OutboxConsumerEnabled
	previousGroup := cfg.OutboxConsumerGroup
	previousName := cfg.OutboxConsumerName
	previousBatchSize := cfg.OutboxConsumerBatchSize
	previousBlockSeconds := cfg.OutboxConsumerBlockSeconds
	previousProcessedTTLSeconds := cfg.OutboxConsumerProcessedTTLSeconds
	previousClaimMinIdleSeconds := cfg.OutboxConsumerClaimMinIdleSeconds
	previousClaimBatchSize := cfg.OutboxConsumerClaimBatchSize
	cfg.OutboxConsumerEnabled = false
	cfg.OutboxConsumerGroup = ""
	cfg.OutboxConsumerName = ""
	cfg.OutboxConsumerBatchSize = 0
	cfg.OutboxConsumerBlockSeconds = 0
	cfg.OutboxConsumerProcessedTTLSeconds = 0
	cfg.OutboxConsumerClaimMinIdleSeconds = 0
	cfg.OutboxConsumerClaimBatchSize = 0
	t.Cleanup(func() {
		cfg.OutboxConsumerEnabled = previousEnabled
		cfg.OutboxConsumerGroup = previousGroup
		cfg.OutboxConsumerName = previousName
		cfg.OutboxConsumerBatchSize = previousBatchSize
		cfg.OutboxConsumerBlockSeconds = previousBlockSeconds
		cfg.OutboxConsumerProcessedTTLSeconds = previousProcessedTTLSeconds
		cfg.OutboxConsumerClaimMinIdleSeconds = previousClaimMinIdleSeconds
		cfg.OutboxConsumerClaimBatchSize = previousClaimBatchSize
	})

	assert.False(t, OutboxConsumerEnabled())
	assert.Equal(t, DefaultOutboxConsumerGroup, OutboxConsumerGroup())
	assert.Equal(t, DefaultOutboxConsumerName, OutboxConsumerName())
	assert.Equal(t, DefaultOutboxConsumerBatchSize, OutboxConsumerBatchSize())
	assert.Equal(t, secondsOrDefault(0, DefaultOutboxConsumerBlockSeconds), OutboxConsumerBlock())
	assert.Equal(t, secondsOrDefault(0, DefaultOutboxConsumerProcessedTTLSeconds), OutboxConsumerProcessedTTL())
	assert.Equal(t, secondsOrDefault(0, DefaultOutboxConsumerClaimMinIdleSeconds), OutboxConsumerClaimMinIdle())
	assert.Equal(t, DefaultOutboxConsumerClaimBatchSize, OutboxConsumerClaimBatchSize())
}
