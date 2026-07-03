package grpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/quangdangfit/gocommon/logger"
	"github.com/quangdangfit/gocommon/validation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	cartGRPC "goshop/internal/cart/port/grpc"
	userGRPC "goshop/internal/user/port/grpc"
	"goshop/pkg/config"
	"goshop/pkg/dbs"
	"goshop/pkg/middleware"
	"goshop/pkg/redis"
)

type Server struct {
	engine    *grpc.Server
	cfg       *config.Schema
	validator validation.Validation
	db        dbs.IDatabase
	cache     redis.IRedis
}

func NewServer(validator validation.Validation, db dbs.IDatabase, cache redis.IRedis) *Server {
	interceptor := middleware.NewAuthInterceptor(config.AuthIgnoreMethods)

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			interceptor.Unary(),
		),
	)

	return &Server{
		engine:    grpcServer,
		cfg:       config.GetConfig(),
		validator: validator,
		db:        db,
		cache:     cache,
	}
}

func (s Server) Run(ctx context.Context) error {
	userGRPC.RegisterHandlers(s.engine, s.db, s.validator)
	cartGRPC.RegisterHandlers(s.engine, s.db, s.validator)

	reflection.Register(s.engine)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.GrpcPort))
	logger.Info("GRPC server is listening on PORT: ", s.cfg.GrpcPort)
	if err != nil {
		logger.Error("Failed to listen: ", err)
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.engine.Serve(lis)
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			return err
		}
		return nil
	case <-ctx.Done():
	}

	stopped := make(chan struct{})
	go func() {
		s.engine.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		s.engine.Stop()
		<-stopped
	}

	if err := <-errCh; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return err
	}

	return nil
}
