package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/env"
	"github.com/joho/godotenv"
)

const (
	DevelopmentEnv = "development"
	TestEnv        = "test"
	ProductionEnv  = "production"

	MinimumProductionAuthSecretBytes = 32

	DatabaseTimeout    = 5 * time.Second
	ProductCachingTime = 1 * time.Minute

	DefaultHTTPReadTimeoutSeconds            = 10
	DefaultHTTPWriteTimeoutSeconds           = 30
	DefaultHTTPIdleTimeoutSeconds            = 60
	DefaultHTTPReadHeaderTimeoutSeconds      = 5
	DefaultHTTPMaxHeaderBytes                = 1 << 20
	DefaultMaxRequestBodyBytes               = 1 << 20
	DefaultMetricsPath                       = "/metrics"
	DefaultOrderIdempotencyTTLSeconds        = 24 * 60 * 60
	DefaultOrderRateLimitLimit               = 120
	DefaultOrderRateLimitWindowSeconds       = 60
	DefaultOutboxPublishBatchSize            = 100
	DefaultOutboxPublishMaxAttempts          = 3
	DefaultOutboxPublishRetryBaseSeconds     = 60
	DefaultOutboxPublishIntervalSeconds      = 30
	DefaultOutboxProcessingTimeoutSeconds    = 15 * 60
	OutboxPublisherTypeLog                   = "log"
	OutboxPublisherTypeRedisStream           = "redis_stream"
	DefaultOutboxPublisherType               = OutboxPublisherTypeLog
	DefaultOutboxRedisStreamName             = "stream:orders"
	DefaultOutboxConsumerGroup               = "order-events"
	DefaultOutboxConsumerName                = "local-consumer-1"
	DefaultOutboxConsumerBatchSize           = 10
	DefaultOutboxConsumerBlockSeconds        = 5
	DefaultOutboxConsumerProcessedTTLSeconds = 24 * 60 * 60
	DefaultOutboxConsumerClaimMinIdleSeconds = 60
	DefaultOutboxConsumerClaimBatchSize      = 10
	DefaultOutboxConsumerMaxDeliveryAttempts = 5
	DefaultOutboxConsumerFailureTTLSeconds   = 24 * 60 * 60
	DefaultOutboxDeadLetterStreamName        = "stream:orders:dead_letter"
	DefaultDatabaseMaxOpenConns              = 25
	DefaultDatabaseMaxIdleConns              = 5
	DefaultDatabaseConnMaxLifetimeSeconds    = 5 * 60
	DefaultDatabaseConnMaxIdleTimeSeconds    = 60
)

type Schema struct {
	Environment           string `env:"environment"`
	HttpPort              int    `env:"http_port"`
	GrpcPort              int    `env:"grpc_port"`
	AuthSecret            string `env:"auth_secret"`
	DatabaseURI           string `env:"database_uri"`
	RedisURI              string `env:"redis_uri"`
	RedisPassword         string `env:"redis_password"`
	RedisDB               int    `env:"redis_db"`
	GRPCReflectionEnabled string `env:"grpc_reflection_enabled"`
	SwaggerEnabled        string `env:"swagger_enabled"`

	DatabaseAutoMigrate            bool `env:"database_auto_migrate"`
	DatabaseMaxOpenConns           int  `env:"database_max_open_conns"`
	DatabaseMaxIdleConns           int  `env:"database_max_idle_conns"`
	DatabaseConnMaxLifetimeSeconds int  `env:"database_conn_max_lifetime_seconds"`
	DatabaseConnMaxIdleTimeSeconds int  `env:"database_conn_max_idle_time_seconds"`

	HTTPReadTimeoutSeconds       int    `env:"http_read_timeout_seconds"`
	HTTPWriteTimeoutSeconds      int    `env:"http_write_timeout_seconds"`
	HTTPIdleTimeoutSeconds       int    `env:"http_idle_timeout_seconds"`
	HTTPReadHeaderTimeoutSeconds int    `env:"http_read_header_timeout_seconds"`
	HTTPMaxHeaderBytes           int    `env:"http_max_header_bytes"`
	MaxRequestBodyBytes          int64  `env:"max_request_body_bytes"`
	MetricsEnabled               string `env:"metrics_enabled"`
	MetricsPath                  string `env:"metrics_path"`

	OrderIdempotencyTTLSeconds  int   `env:"order_idempotency_ttl_seconds"`
	OrderRateLimitLimit         int64 `env:"order_rate_limit_limit"`
	OrderRateLimitWindowSeconds int   `env:"order_rate_limit_window_seconds"`

	OutboxPublisherEnabled         bool   `env:"outbox_publisher_enabled"`
	OutboxPublisherType            string `env:"outbox_publisher_type"`
	OutboxRedisStreamName          string `env:"outbox_redis_stream_name"`
	OutboxPublishBatchSize         int    `env:"outbox_publish_batch_size"`
	OutboxPublishMaxAttempts       int    `env:"outbox_publish_max_attempts"`
	OutboxPublishRetryBaseSeconds  int    `env:"outbox_publish_retry_base_seconds"`
	OutboxPublishIntervalSeconds   int    `env:"outbox_publish_interval_seconds"`
	OutboxProcessingTimeoutSeconds int    `env:"outbox_processing_timeout_seconds"`

	OutboxConsumerEnabled             bool   `env:"outbox_consumer_enabled"`
	OutboxConsumerGroup               string `env:"outbox_consumer_group"`
	OutboxConsumerName                string `env:"outbox_consumer_name"`
	OutboxConsumerBatchSize           int    `env:"outbox_consumer_batch_size"`
	OutboxConsumerBlockSeconds        int    `env:"outbox_consumer_block_seconds"`
	OutboxConsumerProcessedTTLSeconds int    `env:"outbox_consumer_processed_ttl_seconds"`
	OutboxConsumerClaimMinIdleSeconds int    `env:"outbox_consumer_claim_min_idle_seconds"`
	OutboxConsumerClaimBatchSize      int    `env:"outbox_consumer_claim_batch_size"`
	OutboxConsumerMaxDeliveryAttempts int    `env:"outbox_consumer_max_delivery_attempts"`
	OutboxConsumerFailureTTLSeconds   int    `env:"outbox_consumer_failure_ttl_seconds"`
	OutboxDeadLetterStreamName        string `env:"outbox_dead_letter_stream_name"`
}

var (
	cfg Schema
)

func LoadConfig() *Schema {
	if os.Getenv("environment") != ProductionEnv {
		_, filename, _, _ := runtime.Caller(0)
		currentDir := filepath.Dir(filename)
		if err := godotenv.Load(filepath.Join(currentDir, "config.yaml")); err != nil {
			log.Printf("Error on load configuration file, error: %v", err)
		}
	}

	if err := env.Parse(&cfg); err != nil {
		log.Fatalf("Error on parsing configuration file, error: %v", err)
	}
	if err := Validate(cfg); err != nil {
		log.Fatalf("Invalid configuration, error: %v", err)
	}

	return &cfg
}

func Validate(cfg Schema) error {
	switch cfg.Environment {
	case DevelopmentEnv, TestEnv, ProductionEnv:
	default:
		return fmt.Errorf("environment must be one of development, test, or production")
	}
	if err := validateDatabasePoolConfig(cfg); err != nil {
		return err
	}
	if err := validateOptionalBool("metrics_enabled", cfg.MetricsEnabled); err != nil {
		return err
	}
	if err := validateOptionalBool("grpc_reflection_enabled", cfg.GRPCReflectionEnabled); err != nil {
		return err
	}
	if err := validateOptionalBool("swagger_enabled", cfg.SwaggerEnabled); err != nil {
		return err
	}
	if cfg.Environment != ProductionEnv {
		return nil
	}
	if isPlaceholderAuthSecret(cfg.AuthSecret) {
		return fmt.Errorf("production auth_secret must be set to a non-placeholder value")
	}
	if len([]byte(strings.TrimSpace(cfg.AuthSecret))) < MinimumProductionAuthSecretBytes {
		return fmt.Errorf("production auth_secret must be at least %d bytes", MinimumProductionAuthSecretBytes)
	}
	if cfg.HttpPort <= 0 {
		return fmt.Errorf("production http_port must be greater than zero")
	}
	if cfg.GrpcPort <= 0 {
		return fmt.Errorf("production grpc_port must be greater than zero")
	}
	if strings.TrimSpace(cfg.DatabaseURI) == "" {
		return fmt.Errorf("production database_uri is required")
	}
	if strings.TrimSpace(cfg.RedisURI) == "" {
		return fmt.Errorf("production redis_uri is required")
	}
	if cfg.DatabaseAutoMigrate {
		return fmt.Errorf("production database_auto_migrate must be false")
	}
	return nil
}

func validateOptionalBool(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	if _, err := strconv.ParseBool(value); err != nil {
		return fmt.Errorf("%s must be true or false", name)
	}
	return nil
}

func validateDatabasePoolConfig(cfg Schema) error {
	values := []struct {
		name  string
		value int
	}{
		{name: "database_max_open_conns", value: cfg.DatabaseMaxOpenConns},
		{name: "database_max_idle_conns", value: cfg.DatabaseMaxIdleConns},
		{name: "database_conn_max_lifetime_seconds", value: cfg.DatabaseConnMaxLifetimeSeconds},
		{name: "database_conn_max_idle_time_seconds", value: cfg.DatabaseConnMaxIdleTimeSeconds},
	}
	for _, item := range values {
		if item.value < 0 {
			return fmt.Errorf("%s must not be negative", item.name)
		}
	}

	maxOpen := intOrDefault(cfg.DatabaseMaxOpenConns, DefaultDatabaseMaxOpenConns)
	maxIdle := intOrDefault(cfg.DatabaseMaxIdleConns, DefaultDatabaseMaxIdleConns)
	if maxIdle > maxOpen {
		return fmt.Errorf("database_max_idle_conns must not exceed database_max_open_conns")
	}

	return nil
}

func isPlaceholderAuthSecret(secret string) bool {
	switch strings.ToLower(strings.TrimSpace(secret)) {
	case "", "######", "auth_secret", "secret", "change-me", "changeme", "local-dev-secret", "replace-with-a-random-secret":
		return true
	default:
		return false
	}
}

func GetConfig() *Schema {
	return &cfg
}

func DatabaseAutoMigrate() bool {
	return cfg.DatabaseAutoMigrate
}

func DatabaseMaxOpenConns() int {
	return intOrDefault(cfg.DatabaseMaxOpenConns, DefaultDatabaseMaxOpenConns)
}

func DatabaseMaxIdleConns() int {
	return intOrDefault(cfg.DatabaseMaxIdleConns, DefaultDatabaseMaxIdleConns)
}

func DatabaseConnMaxLifetime() time.Duration {
	return secondsOrDefault(cfg.DatabaseConnMaxLifetimeSeconds, DefaultDatabaseConnMaxLifetimeSeconds)
}

func DatabaseConnMaxIdleTime() time.Duration {
	return secondsOrDefault(cfg.DatabaseConnMaxIdleTimeSeconds, DefaultDatabaseConnMaxIdleTimeSeconds)
}

func HTTPReadTimeout() time.Duration {
	return secondsOrDefault(cfg.HTTPReadTimeoutSeconds, DefaultHTTPReadTimeoutSeconds)
}

func HTTPWriteTimeout() time.Duration {
	return secondsOrDefault(cfg.HTTPWriteTimeoutSeconds, DefaultHTTPWriteTimeoutSeconds)
}

func HTTPIdleTimeout() time.Duration {
	return secondsOrDefault(cfg.HTTPIdleTimeoutSeconds, DefaultHTTPIdleTimeoutSeconds)
}

func HTTPReadHeaderTimeout() time.Duration {
	return secondsOrDefault(cfg.HTTPReadHeaderTimeoutSeconds, DefaultHTTPReadHeaderTimeoutSeconds)
}

func HTTPMaxHeaderBytes() int {
	if cfg.HTTPMaxHeaderBytes <= 0 {
		return DefaultHTTPMaxHeaderBytes
	}
	return cfg.HTTPMaxHeaderBytes
}

func MaxRequestBodyBytes() int64 {
	if cfg.MaxRequestBodyBytes <= 0 {
		return DefaultMaxRequestBodyBytes
	}
	return cfg.MaxRequestBodyBytes
}

func MetricsEnabled() bool {
	if strings.TrimSpace(cfg.MetricsEnabled) == "" {
		return cfg.Environment != ProductionEnv
	}
	enabled, _ := strconv.ParseBool(cfg.MetricsEnabled)
	return enabled
}

func GRPCReflectionEnabled() bool {
	if strings.TrimSpace(cfg.GRPCReflectionEnabled) != "" {
		enabled, _ := strconv.ParseBool(cfg.GRPCReflectionEnabled)
		return enabled
	}
	return cfg.Environment != ProductionEnv
}

func SwaggerEnabled() bool {
	if strings.TrimSpace(cfg.SwaggerEnabled) != "" {
		enabled, _ := strconv.ParseBool(cfg.SwaggerEnabled)
		return enabled
	}
	return cfg.Environment != ProductionEnv
}

func MetricsPath() string {
	if cfg.MetricsPath == "" {
		return DefaultMetricsPath
	}
	return cfg.MetricsPath
}

func OrderIdempotencyTTL() time.Duration {
	return secondsOrDefault(cfg.OrderIdempotencyTTLSeconds, DefaultOrderIdempotencyTTLSeconds)
}

func OrderRateLimitLimit() int64 {
	if cfg.OrderRateLimitLimit <= 0 {
		return DefaultOrderRateLimitLimit
	}
	return cfg.OrderRateLimitLimit
}

func OrderRateLimitWindow() time.Duration {
	return secondsOrDefault(cfg.OrderRateLimitWindowSeconds, DefaultOrderRateLimitWindowSeconds)
}

func OutboxPublisherEnabled() bool {
	return cfg.OutboxPublisherEnabled
}

func OutboxPublisherType() string {
	if cfg.OutboxPublisherType == "" {
		return DefaultOutboxPublisherType
	}
	return cfg.OutboxPublisherType
}

func OutboxRedisStreamName() string {
	if cfg.OutboxRedisStreamName == "" {
		return DefaultOutboxRedisStreamName
	}
	return cfg.OutboxRedisStreamName
}

func OutboxPublishBatchSize() int {
	if cfg.OutboxPublishBatchSize <= 0 {
		return DefaultOutboxPublishBatchSize
	}
	return cfg.OutboxPublishBatchSize
}

func OutboxPublishMaxAttempts() int {
	if cfg.OutboxPublishMaxAttempts <= 0 {
		return DefaultOutboxPublishMaxAttempts
	}
	return cfg.OutboxPublishMaxAttempts
}

func OutboxPublishRetryBase() time.Duration {
	return secondsOrDefault(cfg.OutboxPublishRetryBaseSeconds, DefaultOutboxPublishRetryBaseSeconds)
}

func OutboxPublishInterval() time.Duration {
	return secondsOrDefault(cfg.OutboxPublishIntervalSeconds, DefaultOutboxPublishIntervalSeconds)
}

func OutboxProcessingTimeout() time.Duration {
	return secondsOrDefault(cfg.OutboxProcessingTimeoutSeconds, DefaultOutboxProcessingTimeoutSeconds)
}

func OutboxConsumerEnabled() bool {
	return cfg.OutboxConsumerEnabled
}

func OutboxConsumerGroup() string {
	if cfg.OutboxConsumerGroup == "" {
		return DefaultOutboxConsumerGroup
	}
	return cfg.OutboxConsumerGroup
}

func OutboxConsumerName() string {
	if cfg.OutboxConsumerName == "" {
		return DefaultOutboxConsumerName
	}
	return cfg.OutboxConsumerName
}

func OutboxConsumerBatchSize() int {
	if cfg.OutboxConsumerBatchSize <= 0 {
		return DefaultOutboxConsumerBatchSize
	}
	return cfg.OutboxConsumerBatchSize
}

func OutboxConsumerBlock() time.Duration {
	return secondsOrDefault(cfg.OutboxConsumerBlockSeconds, DefaultOutboxConsumerBlockSeconds)
}

func OutboxConsumerProcessedTTL() time.Duration {
	return secondsOrDefault(cfg.OutboxConsumerProcessedTTLSeconds, DefaultOutboxConsumerProcessedTTLSeconds)
}

func OutboxConsumerClaimMinIdle() time.Duration {
	return secondsOrDefault(cfg.OutboxConsumerClaimMinIdleSeconds, DefaultOutboxConsumerClaimMinIdleSeconds)
}

func OutboxConsumerClaimBatchSize() int {
	if cfg.OutboxConsumerClaimBatchSize <= 0 {
		return DefaultOutboxConsumerClaimBatchSize
	}
	return cfg.OutboxConsumerClaimBatchSize
}

func OutboxConsumerMaxDeliveryAttempts() int {
	if cfg.OutboxConsumerMaxDeliveryAttempts <= 0 {
		return DefaultOutboxConsumerMaxDeliveryAttempts
	}
	return cfg.OutboxConsumerMaxDeliveryAttempts
}

func OutboxConsumerFailureTTL() time.Duration {
	return secondsOrDefault(cfg.OutboxConsumerFailureTTLSeconds, DefaultOutboxConsumerFailureTTLSeconds)
}

func OutboxDeadLetterStreamName() string {
	if cfg.OutboxDeadLetterStreamName == "" {
		return DefaultOutboxDeadLetterStreamName
	}
	return cfg.OutboxDeadLetterStreamName
}

func secondsOrDefault(value, defaultValue int) time.Duration {
	if value <= 0 {
		value = defaultValue
	}
	return time.Duration(value) * time.Second
}

func intOrDefault(value, defaultValue int) int {
	if value == 0 {
		return defaultValue
	}
	return value
}
