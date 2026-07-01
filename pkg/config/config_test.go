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
