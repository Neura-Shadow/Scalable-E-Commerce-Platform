package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/quangdangfit/gocommon/logger"
	"github.com/quangdangfit/gocommon/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"goshop/pkg/config"
	dbMocks "goshop/pkg/dbs/mocks"
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

func TestServer_RunStopsGracefullyWhenContextCancels(t *testing.T) {
	cfg := config.GetConfig()
	previousPort := cfg.GrpcPort
	cfg.GrpcPort = 0
	t.Cleanup(func() {
		cfg.GrpcPort = previousPort
	})
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
	case <-time.After(12 * time.Second):
		t.Fatal("gRPC server did not stop after context cancellation")
	}
}
