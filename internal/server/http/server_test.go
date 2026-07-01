package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/quangdangfit/gocommon/validation"
	"github.com/stretchr/testify/assert"
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
