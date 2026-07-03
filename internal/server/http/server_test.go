package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/quangdangfit/gocommon/logger"
	"github.com/quangdangfit/gocommon/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	outboxConsumer "goshop/internal/outbox/consumer"
	outboxService "goshop/internal/outbox/service"
	"goshop/pkg/config"
	dbMocks "goshop/pkg/dbs/mocks"
	appMetrics "goshop/pkg/metrics"
	redisMocks "goshop/pkg/redis/mocks"
)

func init() {
	logger.Initialize(config.ProductionEnv)
}

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

func TestServer_MetricsRouteEnabledByDefault(t *testing.T) {
	withMetricsConfig(t, nil, "")
	appMetrics.ResetForTest()
	mockDB := dbMocks.NewIDatabase(t)
	mockRedis := redisMocks.NewIRedis(t)

	server := NewServer(validation.New(), mockDB, mockRedis)
	err := server.MapRoutes()
	require.NoError(t, err)

	server.GetEngine().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	writer := httptest.NewRecorder()
	server.GetEngine().ServeHTTP(writer, req)

	assert.Equal(t, http.StatusOK, writer.Code)
	assert.Contains(t, writer.Body.String(), "http_requests_total")
}

func TestServer_MetricsRouteCanBeDisabled(t *testing.T) {
	disabled := false
	withMetricsConfig(t, &disabled, "/metrics")
	mockDB := dbMocks.NewIDatabase(t)
	mockRedis := redisMocks.NewIRedis(t)

	server := NewServer(validation.New(), mockDB, mockRedis)
	err := server.MapRoutes()
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	writer := httptest.NewRecorder()
	server.GetEngine().ServeHTTP(writer, req)

	assert.Equal(t, http.StatusNotFound, writer.Code)
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
	withOutboxPublisherConfig(t, false, "log", "")
	mockDB := dbMocks.NewIDatabase(t)
	mockRedis := redisMocks.NewIRedis(t)
	server := NewServer(validation.New(), mockDB, mockRedis)

	publisher, err := server.newOutboxPublisher()

	require.NoError(t, err)
	assert.IsType(t, &outboxService.LogPublisher{}, publisher)
}

func TestServer_NewOutboxPublisherChoosesRedisStreamPublisher(t *testing.T) {
	withOutboxPublisherConfig(t, false, "redis_stream", "stream:test-orders")
	mockDB := dbMocks.NewIDatabase(t)
	mockRedis := redisMocks.NewIRedis(t)
	server := NewServer(validation.New(), mockDB, mockRedis)

	publisher, err := server.newOutboxPublisher()

	require.NoError(t, err)
	assert.IsType(t, &outboxService.RedisStreamPublisher{}, publisher)
}

func TestServer_StartOutboxPublisherRejectsUnknownPublisherType(t *testing.T) {
	withOutboxPublisherConfig(t, true, "unknown", "")
	mockDB := dbMocks.NewIDatabase(t)
	mockRedis := redisMocks.NewIRedis(t)
	server := NewServer(validation.New(), mockDB, mockRedis)

	stop, err := server.startOutboxPublisher(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown outbox publisher type")
	require.NotNil(t, stop)
	require.NoError(t, stop())
}

func TestServer_StartOutboxPublisherDisabledDoesNotValidatePublisherType(t *testing.T) {
	withOutboxPublisherConfig(t, false, "unknown", "")
	mockDB := dbMocks.NewIDatabase(t)
	mockRedis := redisMocks.NewIRedis(t)
	server := NewServer(validation.New(), mockDB, mockRedis)

	stop, err := server.startOutboxPublisher(context.Background())

	require.NoError(t, err)
	require.NotNil(t, stop)
	require.NoError(t, stop())
}

func TestServer_StartOutboxConsumerDisabledByDefault(t *testing.T) {
	withOutboxConsumerConfig(t, false, "stream:orders", "order-events", "local-consumer-1", 10, 5, 86400, 60, 10)
	mockDB := dbMocks.NewIDatabase(t)
	mockRedis := redisMocks.NewIRedis(t)
	server := NewServer(validation.New(), mockDB, mockRedis)

	stop, err := server.startOutboxConsumer(context.Background())

	require.NoError(t, err)
	require.NotNil(t, stop)
	require.NoError(t, stop())
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

	ctx, cancel := context.WithCancel(context.Background())
	stop, err := server.startOutboxConsumer(ctx)

	require.NoError(t, err)
	require.NotNil(t, stop)
	cancel()
	require.NoError(t, stop())
}

func TestServer_StartOutboxConsumerEnabledReturnsClearConfigError(t *testing.T) {
	withOutboxConsumerConfig(t, true, "stream:test-orders", "test-group", "test-consumer", 3, 1, 120, 7, 2)
	mockDB := dbMocks.NewIDatabase(t)
	server := NewServer(validation.New(), mockDB, nil)

	stop, err := server.startOutboxConsumer(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis cache is required")
	require.NotNil(t, stop)
	require.NoError(t, stop())
}

type fakePublisherLoop struct {
	entered chan struct{}
	err     error
}

func (l *fakePublisherLoop) RunOnce(ctx context.Context) (outboxService.PublishBatchResult, error) {
	close(l.entered)
	<-ctx.Done()
	return outboxService.PublishBatchResult{}, l.err
}

func TestRunOutboxPublisherLoopExitsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	loop := &fakePublisherLoop{entered: make(chan struct{})}

	done := runOutboxPublisherLoop(ctx, loop, time.Hour)
	<-loop.entered
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publisher loop did not exit after context cancellation")
	}
}

type fakeConsumerLoop struct {
	readEntered chan struct{}
}

func (l *fakeConsumerLoop) RunOnce(ctx context.Context) (outboxConsumer.BatchResult, error) {
	close(l.readEntered)
	<-ctx.Done()
	return outboxConsumer.BatchResult{}, errors.New("context done")
}

func (l *fakeConsumerLoop) ClaimStaleOnce(ctx context.Context) (outboxConsumer.BatchResult, error) {
	return outboxConsumer.BatchResult{}, nil
}

func TestRunOutboxConsumerLoopExitsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	loop := &fakeConsumerLoop{readEntered: make(chan struct{})}

	done := runOutboxConsumerLoop(ctx, loop)
	<-loop.readEntered
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("consumer loop did not exit after context cancellation")
	}
}

func TestServer_RunShutsDownHTTPWhenContextCancels(t *testing.T) {
	withHTTPPort(t, 0)
	withOutboxPublisherConfig(t, false, "log", "")
	withOutboxConsumerConfig(t, false, "stream:orders", "order-events", "local-consumer-1", 10, 5, 86400, 60, 10)
	mockDB := dbMocks.NewIDatabase(t)
	mockRedis := redisMocks.NewIRedis(t)
	server := NewServer(validation.New(), mockDB, mockRedis)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- server.Run(ctx)
	}()
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("HTTP server did not shut down after context cancellation")
	}
}

func withOutboxPublisherConfig(t *testing.T, enabled bool, publisherType string, streamName string) {
	t.Helper()
	cfg := config.GetConfig()
	previousPublisherType := cfg.OutboxPublisherType
	previousStreamName := cfg.OutboxRedisStreamName
	previousEnabled := cfg.OutboxPublisherEnabled

	cfg.OutboxPublisherType = publisherType
	cfg.OutboxRedisStreamName = streamName
	cfg.OutboxPublisherEnabled = enabled

	t.Cleanup(func() {
		cfg.OutboxPublisherType = previousPublisherType
		cfg.OutboxRedisStreamName = previousStreamName
		cfg.OutboxPublisherEnabled = previousEnabled
	})
}

func withHTTPPort(t *testing.T, port int) {
	t.Helper()
	cfg := config.GetConfig()
	previousPort := cfg.HttpPort
	cfg.HttpPort = port
	t.Cleanup(func() {
		cfg.HttpPort = previousPort
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
	previousMaxDeliveryAttempts := cfg.OutboxConsumerMaxDeliveryAttempts
	previousFailureTTLSeconds := cfg.OutboxConsumerFailureTTLSeconds
	previousDeadLetterStreamName := cfg.OutboxDeadLetterStreamName

	cfg.OutboxRedisStreamName = streamName
	cfg.OutboxConsumerEnabled = enabled
	cfg.OutboxConsumerGroup = groupName
	cfg.OutboxConsumerName = consumerName
	cfg.OutboxConsumerBatchSize = batchSize
	cfg.OutboxConsumerBlockSeconds = blockSeconds
	cfg.OutboxConsumerProcessedTTLSeconds = processedTTLSeconds
	cfg.OutboxConsumerClaimMinIdleSeconds = claimMinIdleSeconds
	cfg.OutboxConsumerClaimBatchSize = claimBatchSize
	cfg.OutboxConsumerMaxDeliveryAttempts = 5
	cfg.OutboxConsumerFailureTTLSeconds = 86400
	cfg.OutboxDeadLetterStreamName = "stream:orders:dead_letter"

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
		cfg.OutboxConsumerMaxDeliveryAttempts = previousMaxDeliveryAttempts
		cfg.OutboxConsumerFailureTTLSeconds = previousFailureTTLSeconds
		cfg.OutboxDeadLetterStreamName = previousDeadLetterStreamName
	})
}

func withMetricsConfig(t *testing.T, enabled *bool, path string) {
	t.Helper()
	cfg := config.GetConfig()
	previousEnabled := cfg.MetricsEnabled
	previousPath := cfg.MetricsPath

	cfg.MetricsEnabled = enabled
	cfg.MetricsPath = path

	t.Cleanup(func() {
		cfg.MetricsEnabled = previousEnabled
		cfg.MetricsPath = previousPath
	})
}
