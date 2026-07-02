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
	previousMaxDeliveryAttempts := cfg.OutboxConsumerMaxDeliveryAttempts
	previousFailureTTLSeconds := cfg.OutboxConsumerFailureTTLSeconds
	previousDeadLetterStreamName := cfg.OutboxDeadLetterStreamName
	cfg.OutboxConsumerEnabled = false
	cfg.OutboxConsumerGroup = ""
	cfg.OutboxConsumerName = ""
	cfg.OutboxConsumerBatchSize = 0
	cfg.OutboxConsumerBlockSeconds = 0
	cfg.OutboxConsumerProcessedTTLSeconds = 0
	cfg.OutboxConsumerClaimMinIdleSeconds = 0
	cfg.OutboxConsumerClaimBatchSize = 0
	cfg.OutboxConsumerMaxDeliveryAttempts = 0
	cfg.OutboxConsumerFailureTTLSeconds = 0
	cfg.OutboxDeadLetterStreamName = ""
	t.Cleanup(func() {
		cfg.OutboxConsumerEnabled = previousEnabled
		cfg.OutboxConsumerGroup = previousGroup
		cfg.OutboxConsumerName = previousName
		cfg.OutboxConsumerBatchSize = previousBatchSize
		cfg.OutboxConsumerBlockSeconds = previousBlockSeconds
		cfg.OutboxConsumerProcessedTTLSeconds = previousProcessedTTLSeconds
		cfg.OutboxConsumerClaimMinIdleSeconds = previousClaimMinIdleSeconds
		cfg.OutboxConsumerClaimBatchSize = previousClaimBatchSize
		cfg.OutboxConsumerMaxDeliveryAttempts = previousMaxDeliveryAttempts
		cfg.OutboxConsumerFailureTTLSeconds = previousFailureTTLSeconds
		cfg.OutboxDeadLetterStreamName = previousDeadLetterStreamName
	})

	assert.False(t, OutboxConsumerEnabled())
	assert.Equal(t, DefaultOutboxConsumerGroup, OutboxConsumerGroup())
	assert.Equal(t, DefaultOutboxConsumerName, OutboxConsumerName())
	assert.Equal(t, DefaultOutboxConsumerBatchSize, OutboxConsumerBatchSize())
	assert.Equal(t, secondsOrDefault(0, DefaultOutboxConsumerBlockSeconds), OutboxConsumerBlock())
	assert.Equal(t, secondsOrDefault(0, DefaultOutboxConsumerProcessedTTLSeconds), OutboxConsumerProcessedTTL())
	assert.Equal(t, secondsOrDefault(0, DefaultOutboxConsumerClaimMinIdleSeconds), OutboxConsumerClaimMinIdle())
	assert.Equal(t, DefaultOutboxConsumerClaimBatchSize, OutboxConsumerClaimBatchSize())
	assert.Equal(t, DefaultOutboxConsumerMaxDeliveryAttempts, OutboxConsumerMaxDeliveryAttempts())
	assert.Equal(t, secondsOrDefault(0, DefaultOutboxConsumerFailureTTLSeconds), OutboxConsumerFailureTTL())
	assert.Equal(t, DefaultOutboxDeadLetterStreamName, OutboxDeadLetterStreamName())
}

func TestMetricsConfigDefaults(t *testing.T) {
	previousEnabled := cfg.MetricsEnabled
	previousPath := cfg.MetricsPath
	cfg.MetricsEnabled = nil
	cfg.MetricsPath = ""
	t.Cleanup(func() {
		cfg.MetricsEnabled = previousEnabled
		cfg.MetricsPath = previousPath
	})

	assert.True(t, MetricsEnabled())
	assert.Equal(t, DefaultMetricsPath, MetricsPath())
}

func TestMetricsConfigCanBeDisabled(t *testing.T) {
	previousEnabled := cfg.MetricsEnabled
	disabled := false
	cfg.MetricsEnabled = &disabled
	t.Cleanup(func() {
		cfg.MetricsEnabled = previousEnabled
	})

	assert.False(t, MetricsEnabled())
}
