package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/quangdangfit/gocommon/validation"
	"github.com/stretchr/testify/assert"

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
