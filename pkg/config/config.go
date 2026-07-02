package config

import (
	"log"
	"path/filepath"
	"runtime"
	"time"

	"github.com/caarlos0/env"
	"github.com/joho/godotenv"
)

const (
	ProductionEnv = "production"

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
)

var AuthIgnoreMethods = []string{
	"/user.UserService/Login",
	"/user.UserService/Register",
}

type Schema struct {
	Environment   string `env:"environment"`
	HttpPort      int    `env:"http_port"`
	GrpcPort      int    `env:"grpc_port"`
	AuthSecret    string `env:"auth_secret"`
	DatabaseURI   string `env:"database_uri"`
	RedisURI      string `env:"redis_uri"`
	RedisPassword string `env:"redis_password"`
	RedisDB       int    `env:"redis_db"`

	HTTPReadTimeoutSeconds       int    `env:"http_read_timeout_seconds"`
	HTTPWriteTimeoutSeconds      int    `env:"http_write_timeout_seconds"`
	HTTPIdleTimeoutSeconds       int    `env:"http_idle_timeout_seconds"`
	HTTPReadHeaderTimeoutSeconds int    `env:"http_read_header_timeout_seconds"`
	HTTPMaxHeaderBytes           int    `env:"http_max_header_bytes"`
	MaxRequestBodyBytes          int64  `env:"max_request_body_bytes"`
	MetricsEnabled               *bool  `env:"metrics_enabled"`
	MetricsPath                  string `env:"metrics_path"`

	OrderIdempotencyTTLSeconds  int   `env:"order_idempotency_ttl_seconds"`
	OrderRateLimitLimit         int64 `env:"order_rate_limit_limit"`
	OrderRateLimitWindowSeconds int   `env:"order_rate_limit_window_seconds"`

	OutboxPublisherEnabled        bool   `env:"outbox_publisher_enabled"`
	OutboxPublisherType           string `env:"outbox_publisher_type"`
	OutboxRedisStreamName         string `env:"outbox_redis_stream_name"`
	OutboxPublishBatchSize        int    `env:"outbox_publish_batch_size"`
	OutboxPublishMaxAttempts      int    `env:"outbox_publish_max_attempts"`
	OutboxPublishRetryBaseSeconds int    `env:"outbox_publish_retry_base_seconds"`
	OutboxPublishIntervalSeconds  int    `env:"outbox_publish_interval_seconds"`

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
	_, filename, _, _ := runtime.Caller(0)
	currentDir := filepath.Dir(filename)

	err := godotenv.Load(filepath.Join(currentDir, "config.yaml"))
	if err != nil {
		log.Printf("Error on load configuration file, error: %v", err)
	}

	if err := env.Parse(&cfg); err != nil {
		log.Fatalf("Error on parsing configuration file, error: %v", err)
	}

	return &cfg
}

func GetConfig() *Schema {
	return &cfg
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
	if cfg.MetricsEnabled == nil {
		return true
	}
	return *cfg.MetricsEnabled
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
