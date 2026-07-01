package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/quangdangfit/gocommon/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	outboxService "goshop/internal/outbox/service"
	"goshop/pkg/config"
	dbMocks "goshop/pkg/dbs/mocks"
	redisMocks "goshop/pkg/redis/mocks"
)

func TestNewServer(t *testing.T) {
	mockDB := dbMocks.NewIDatabase(t)
	mockRedis := redisMocks.NewIRedis(t)

	server := NewServer(validation.New(), mockDB, mockRedis)
	assert.NotNil(t, server)
}

func TestServer_GetEngine(t *testing.T) {
	mockDB := dbMocks.NewIDatabase(t)
	mockRedis := redisMocks.NewIRedis(t)

	server := NewServer(validation.New(), mockDB, mockRedis)
	assert.NotNil(t, server)

	engine := server.GetEngine()
	assert.NotNil(t, engine)
}

func TestServer_MapRoutes(t *testing.T) {
	mockDB := dbMocks.NewIDatabase(t)
	mockRedis := redisMocks.NewIRedis(t)

	server := NewServer(validation.New(), mockDB, mockRedis)
	assert.NotNil(t, server)

	err := server.MapRoutes()
	assert.Nil(t, err)
}

func TestServer_HealthRoute(t *testing.T) {
	mockDB := dbMocks.NewIDatabase(t)
	mockRedis := redisMocks.NewIRedis(t)

	server := NewServer(validation.New(), mockDB, mockRedis)
	err := server.MapRoutes()
	assert.Nil(t, err)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	writer := httptest.NewRecorder()
	server.GetEngine().ServeHTTP(writer, req)
	assert.Equal(t, http.StatusOK, writer.Code)
}

func TestServer_HTTPServerHardeningDefaults(t *testing.T) {
	mockDB := dbMocks.NewIDatabase(t)
	mockRedis := redisMocks.NewIRedis(t)

	server := NewServer(validation.New(), mockDB, mockRedis)
	httpServer := server.newHTTPServer()

	assert.NotZero(t, httpServer.ReadTimeout)
	assert.NotZero(t, httpServer.WriteTimeout)
	assert.NotZero(t, httpServer.IdleTimeout)
	assert.NotZero(t, httpServer.ReadHeaderTimeout)
	assert.NotZero(t, httpServer.MaxHeaderBytes)
}

func TestServer_NewOutboxPublisherChoosesLogPublisher(t *testing.T) {
	withOutboxPublisherConfig(t, "log", "")
	mockDB := dbMocks.NewIDatabase(t)
	mockRedis := redisMocks.NewIRedis(t)
	server := NewServer(validation.New(), mockDB, mockRedis)

	publisher, err := server.newOutboxPublisher()

	require.NoError(t, err)
	assert.IsType(t, &outboxService.LogPublisher{}, publisher)
}

func TestServer_NewOutboxPublisherChoosesRedisStreamPublisher(t *testing.T) {
	withOutboxPublisherConfig(t, "redis_stream", "stream:test-orders")
	mockDB := dbMocks.NewIDatabase(t)
	mockRedis := redisMocks.NewIRedis(t)
	server := NewServer(validation.New(), mockDB, mockRedis)

	publisher, err := server.newOutboxPublisher()

	require.NoError(t, err)
	assert.IsType(t, &outboxService.RedisStreamPublisher{}, publisher)
}

func TestServer_StartOutboxPublisherRejectsUnknownPublisherType(t *testing.T) {
	withOutboxPublisherConfig(t, "unknown", "")
	mockDB := dbMocks.NewIDatabase(t)
	mockRedis := redisMocks.NewIRedis(t)
	server := NewServer(validation.New(), mockDB, mockRedis)

	stop, err := server.startOutboxPublisher()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown outbox publisher type")
	require.NotNil(t, stop)
	stop()
}

func TestServer_StartOutboxConsumerDisabledByDefault(t *testing.T) {
	withOutboxConsumerConfig(t, false, "stream:orders", "order-events", "local-consumer-1", 10, 5, 86400, 60, 10)
	mockDB := dbMocks.NewIDatabase(t)
	mockRedis := redisMocks.NewIRedis(t)
	server := NewServer(validation.New(), mockDB, mockRedis)

	stop, err := server.startOutboxConsumer()

	require.NoError(t, err)
	require.NotNil(t, stop)
	stop()
}

func TestServer_StartOutboxConsumerEnabledUsesConfigValues(t *testing.T) {
	withOutboxConsumerConfig(t, true, "stream:test-orders", "test-group", "test-consumer", 3, 1, 120, 7, 2)
	mockDB := dbMocks.NewIDatabase(t)
	mockRedis := redisMocks.NewIRedis(t)
	mockRedis.On("XGroupCreateMkStream", mock.Anything, "stream:test-orders", "test-group", "0").
		Return(nil).
		Once()
	mockRedis.On("XReadGroup", mock.Anything, "test-group", "test-consumer", "stream:test-orders", int64(3), time.Second).
		Return(nil, nil).
		Maybe()
	mockRedis.On("XAutoClaim", mock.Anything, "stream:test-orders", "test-group", "test-consumer", "0-0", 7*time.Second, int64(2)).
		Return(nil, "0-0", nil).
		Maybe()
	server := NewServer(validation.New(), mockDB, mockRedis)

	stop, err := server.startOutboxConsumer()

	require.NoError(t, err)
	require.NotNil(t, stop)
	stop()
}

func withOutboxPublisherConfig(t *testing.T, publisherType string, streamName string) {
	t.Helper()
	cfg := config.GetConfig()
	previousPublisherType := cfg.OutboxPublisherType
	previousStreamName := cfg.OutboxRedisStreamName
	previousEnabled := cfg.OutboxPublisherEnabled

	cfg.OutboxPublisherType = publisherType
	cfg.OutboxRedisStreamName = streamName
	cfg.OutboxPublisherEnabled = false

	t.Cleanup(func() {
		cfg.OutboxPublisherType = previousPublisherType
		cfg.OutboxRedisStreamName = previousStreamName
		cfg.OutboxPublisherEnabled = previousEnabled
	})
}

func withOutboxConsumerConfig(
	t *testing.T,
	enabled bool,
	streamName string,
	groupName string,
	consumerName string,
	batchSize int,
	blockSeconds int,
	processedTTLSeconds int,
	claimMinIdleSeconds int,
	claimBatchSize int,
) {
	t.Helper()
	cfg := config.GetConfig()
	previousPublisherStreamName := cfg.OutboxRedisStreamName
	previousEnabled := cfg.OutboxConsumerEnabled
	previousGroupName := cfg.OutboxConsumerGroup
	previousConsumerName := cfg.OutboxConsumerName
	previousBatchSize := cfg.OutboxConsumerBatchSize
	previousBlockSeconds := cfg.OutboxConsumerBlockSeconds
	previousProcessedTTLSeconds := cfg.OutboxConsumerProcessedTTLSeconds
	previousClaimMinIdleSeconds := cfg.OutboxConsumerClaimMinIdleSeconds
	previousClaimBatchSize := cfg.OutboxConsumerClaimBatchSize

	cfg.OutboxRedisStreamName = streamName
	cfg.OutboxConsumerEnabled = enabled
	cfg.OutboxConsumerGroup = groupName
	cfg.OutboxConsumerName = consumerName
	cfg.OutboxConsumerBatchSize = batchSize
	cfg.OutboxConsumerBlockSeconds = blockSeconds
	cfg.OutboxConsumerProcessedTTLSeconds = processedTTLSeconds
	cfg.OutboxConsumerClaimMinIdleSeconds = claimMinIdleSeconds
	cfg.OutboxConsumerClaimBatchSize = claimBatchSize

	t.Cleanup(func() {
		cfg.OutboxRedisStreamName = previousPublisherStreamName
		cfg.OutboxConsumerEnabled = previousEnabled
		cfg.OutboxConsumerGroup = previousGroupName
		cfg.OutboxConsumerName = previousConsumerName
		cfg.OutboxConsumerBatchSize = previousBatchSize
		cfg.OutboxConsumerBlockSeconds = previousBlockSeconds
		cfg.OutboxConsumerProcessedTTLSeconds = previousProcessedTTLSeconds
		cfg.OutboxConsumerClaimMinIdleSeconds = previousClaimMinIdleSeconds
		cfg.OutboxConsumerClaimBatchSize = previousClaimBatchSize
	})
}
