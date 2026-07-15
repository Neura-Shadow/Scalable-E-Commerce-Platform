package config

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDatabaseConfigDefaults(t *testing.T) {
	previous := cfg
	cfg = Schema{}
	t.Cleanup(func() { cfg = previous })

	assert.False(t, DatabaseAutoMigrate())
	assert.Equal(t, DefaultDatabaseMaxOpenConns, DatabaseMaxOpenConns())
	assert.Equal(t, DefaultDatabaseMaxIdleConns, DatabaseMaxIdleConns())
	assert.Equal(t, time.Duration(DefaultDatabaseConnMaxLifetimeSeconds)*time.Second, DatabaseConnMaxLifetime())
	assert.Equal(t, time.Duration(DefaultDatabaseConnMaxIdleTimeSeconds)*time.Second, DatabaseConnMaxIdleTime())
}

func TestValidateConfigRejectsDatabaseIdleConnectionsAboveOpenConnections(t *testing.T) {
	err := Validate(Schema{
		Environment:          DevelopmentEnv,
		DatabaseMaxOpenConns: 10,
		DatabaseMaxIdleConns: 11,
	})

	assert.ErrorContains(t, err, "database_max_idle_conns")
}

func TestValidateConfigRejectsNegativeDatabasePoolValues(t *testing.T) {
	tests := []struct {
		name   string
		config Schema
	}{
		{name: "max open", config: Schema{Environment: DevelopmentEnv, DatabaseMaxOpenConns: -1}},
		{name: "max idle", config: Schema{Environment: DevelopmentEnv, DatabaseMaxIdleConns: -1}},
		{name: "max lifetime", config: Schema{Environment: DevelopmentEnv, DatabaseConnMaxLifetimeSeconds: -1}},
		{name: "max idle time", config: Schema{Environment: DevelopmentEnv, DatabaseConnMaxIdleTimeSeconds: -1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Error(t, Validate(tt.config))
		})
	}
}

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
	previousProcessingTimeoutSeconds := cfg.OutboxProcessingTimeoutSeconds
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
	cfg.OutboxProcessingTimeoutSeconds = 0
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
		cfg.OutboxProcessingTimeoutSeconds = previousProcessingTimeoutSeconds
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
	assert.Equal(t, secondsOrDefault(0, DefaultOutboxProcessingTimeoutSeconds), OutboxProcessingTimeout())
}

func TestMetricsConfigDefaults(t *testing.T) {
	previousEnabled := cfg.MetricsEnabled
	previousPath := cfg.MetricsPath
	cfg.MetricsEnabled = ""
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
	cfg.MetricsEnabled = "false"
	t.Cleanup(func() {
		cfg.MetricsEnabled = previousEnabled
	})

	assert.False(t, MetricsEnabled())
}

func TestMetricsDefaultsToDisabledInProduction(t *testing.T) {
	previous := cfg
	cfg = Schema{Environment: ProductionEnv}
	t.Cleanup(func() { cfg = previous })

	assert.False(t, MetricsEnabled())
}

func TestGRPCReflectionDefaultsToDisabledInProduction(t *testing.T) {
	previous := cfg
	cfg = Schema{Environment: ProductionEnv}
	t.Cleanup(func() { cfg = previous })

	assert.False(t, GRPCReflectionEnabled())
}

func TestGRPCReflectionDefaultsToEnabledOutsideProduction(t *testing.T) {
	previous := cfg
	cfg = Schema{Environment: DevelopmentEnv}
	t.Cleanup(func() { cfg = previous })

	assert.True(t, GRPCReflectionEnabled())
}

func TestGRPCReflectionCanBeExplicitlyEnabled(t *testing.T) {
	previous := cfg
	cfg = Schema{Environment: ProductionEnv, GRPCReflectionEnabled: "true"}
	t.Cleanup(func() { cfg = previous })

	assert.True(t, GRPCReflectionEnabled())
}

func TestSwaggerDefaultsToDisabledInProduction(t *testing.T) {
	previous := cfg
	cfg = Schema{Environment: ProductionEnv}
	t.Cleanup(func() { cfg = previous })

	assert.False(t, SwaggerEnabled())
}

func TestSwaggerDefaultsToEnabledOutsideProduction(t *testing.T) {
	previous := cfg
	cfg = Schema{Environment: DevelopmentEnv}
	t.Cleanup(func() { cfg = previous })

	assert.True(t, SwaggerEnabled())
}

func TestSwaggerCanBeExplicitlyEnabled(t *testing.T) {
	previous := cfg
	cfg = Schema{Environment: ProductionEnv, SwaggerEnabled: "true"}
	t.Cleanup(func() { cfg = previous })

	assert.True(t, SwaggerEnabled())
}

func TestValidateConfigRejectsInvalidOptionalBooleans(t *testing.T) {
	assert.Error(t, Validate(Schema{Environment: DevelopmentEnv, MetricsEnabled: "sometimes"}))
	assert.Error(t, Validate(Schema{Environment: DevelopmentEnv, GRPCReflectionEnabled: "sometimes"}))
	assert.Error(t, Validate(Schema{Environment: DevelopmentEnv, SwaggerEnabled: "sometimes"}))
}

func TestValidateConfigRejectsUnknownOrEmptyEnvironment(t *testing.T) {
	assert.ErrorContains(t, Validate(Schema{}), "environment")
	assert.ErrorContains(t, Validate(Schema{Environment: "prodution"}), "environment")
}

func TestValidateConfigRejectsProductionPlaceholderAuthSecret(t *testing.T) {
	err := Validate(Schema{
		Environment: ProductionEnv,
		AuthSecret:  "auth_secret",
	})

	assert.Error(t, err)
}

func TestValidateConfigAllowsDevelopmentPlaceholderAuthSecret(t *testing.T) {
	err := Validate(Schema{
		Environment: DevelopmentEnv,
		AuthSecret:  "auth_secret",
	})

	assert.NoError(t, err)
}

func TestValidateConfigAllowsProductionNonPlaceholderAuthSecret(t *testing.T) {
	err := Validate(Schema{
		Environment: ProductionEnv,
		HttpPort:    8888,
		GrpcPort:    8889,
		AuthSecret:  strings.Repeat("test-only-", 4),
		DatabaseURI: "postgres://configured",
		RedisURI:    "redis:6379",
	})

	assert.NoError(t, err)
}

func TestValidateConfigRejectsShortProductionAuthSecret(t *testing.T) {
	err := Validate(Schema{
		Environment: ProductionEnv,
		HttpPort:    8888,
		GrpcPort:    8889,
		AuthSecret:  "short-but-not-placeholder",
		DatabaseURI: "postgres://configured",
		RedisURI:    "redis:6379",
	})

	assert.ErrorContains(t, err, "at least 32 bytes")
}

func TestValidateConfigRejectsMissingProductionDependencies(t *testing.T) {
	base := Schema{
		Environment: ProductionEnv,
		HttpPort:    8888,
		GrpcPort:    8889,
		AuthSecret:  strings.Repeat("test-only-", 4),
		DatabaseURI: "postgres://configured",
		RedisURI:    "redis:6379",
	}
	tests := []struct {
		name   string
		modify func(*Schema)
	}{
		{name: "HTTP port", modify: func(cfg *Schema) { cfg.HttpPort = 0 }},
		{name: "gRPC port", modify: func(cfg *Schema) { cfg.GrpcPort = 0 }},
		{name: "database URI", modify: func(cfg *Schema) { cfg.DatabaseURI = "" }},
		{name: "Redis URI", modify: func(cfg *Schema) { cfg.RedisURI = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := base
			tt.modify(&candidate)
			assert.Error(t, Validate(candidate))
		})
	}
}
