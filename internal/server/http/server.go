package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/quangdangfit/gocommon/logger"
	"github.com/quangdangfit/gocommon/validation"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "goshop/docs"
	inventoryHttp "goshop/internal/inventory/port/http"
	inventoryRepository "goshop/internal/inventory/repository"
	inventoryService "goshop/internal/inventory/service"
	orderHttp "goshop/internal/order/port/http"
	orderRepository "goshop/internal/order/repository"
	orderService "goshop/internal/order/service"
	outboxConsumer "goshop/internal/outbox/consumer"
	outboxRepository "goshop/internal/outbox/repository"
	outboxService "goshop/internal/outbox/service"
	productHttp "goshop/internal/product/port/http"
	userHttp "goshop/internal/user/port/http"
	"goshop/pkg/config"
	"goshop/pkg/dbs"
	appMetrics "goshop/pkg/metrics"
	"goshop/pkg/middleware"
	"goshop/pkg/redis"
	"goshop/pkg/response"
)

type Server struct {
	engine    *gin.Engine
	cfg       *config.Schema
	validator validation.Validation
	db        dbs.IDatabase
	cache     redis.IRedis
}

type backgroundStopFunc func() error

type publisherRunner interface {
	RunOnce(ctx context.Context) (outboxService.PublishBatchResult, error)
}

type consumerRunner interface {
	RunOnce(ctx context.Context) (outboxConsumer.BatchResult, error)
	ClaimStaleOnce(ctx context.Context) (outboxConsumer.BatchResult, error)
}

func NewServer(validator validation.Validation, db dbs.IDatabase, cache redis.IRedis) *Server {
	engine := gin.Default()
	_ = engine.SetTrustedProxies(nil)
	engine.Use(middleware.MaxBodyBytes(config.MaxRequestBodyBytes()))
	engine.Use(middleware.HTTPMetrics())

	return &Server{
		engine:    engine,
		cfg:       config.GetConfig(),
		validator: validator,
		db:        db,
		cache:     cache,
	}
}

func (s Server) Run(ctx context.Context) error {
	if s.cfg.Environment == config.ProductionEnv {
		gin.SetMode(gin.ReleaseMode)
	}

	if err := s.MapRoutes(); err != nil {
		return fmt.Errorf("map HTTP routes: %w", err)
	}
	s.engine.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	stopOutboxPublisher, err := s.startOutboxPublisher(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err := stopOutboxPublisher(); err != nil {
			logger.Error("outbox_publisher_shutdown_failed", err)
		}
	}()
	stopOutboxConsumer, err := s.startOutboxConsumer(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err := stopOutboxConsumer(); err != nil {
			logger.Error("outbox_consumer_shutdown_failed", err)
		}
	}()

	logger.Info("HTTP server is listening on PORT: ", s.cfg.HttpPort)
	httpServer := s.newHTTPServer()
	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("run HTTP server: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	serveErr := <-errCh
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return fmt.Errorf("run HTTP server: %w", serveErr)
	}

	return nil
}

func (s Server) GetEngine() *gin.Engine {
	return s.engine
}

func (s Server) MapRoutes() error {
	s.engine.GET("/health", func(c *gin.Context) {
		response.JSON(c, http.StatusOK, nil)
	})
	if config.MetricsEnabled() {
		s.engine.GET(config.MetricsPath(), gin.WrapH(appMetrics.Handler()))
	}
	v1 := s.engine.Group("/api/v1")
	userHttp.Routes(v1, s.db, s.validator)
	productHttp.Routes(v1, s.db, s.validator, s.cache)
	orderHttp.Routes(v1, s.newOrderService(), s.cache)
	inventoryHttp.Routes(v1, s.db, s.validator)
	return nil
}

func (s Server) newHTTPServer() *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", s.cfg.HttpPort),
		Handler:           s.engine,
		ReadTimeout:       config.HTTPReadTimeout(),
		WriteTimeout:      config.HTTPWriteTimeout(),
		IdleTimeout:       config.HTTPIdleTimeout(),
		ReadHeaderTimeout: config.HTTPReadHeaderTimeout(),
		MaxHeaderBytes:    config.HTTPMaxHeaderBytes(),
	}
}

func (s Server) newOrderService() orderService.IOrderService {
	orderRepo := orderRepository.NewOrderRepository(s.db)
	productRepo := orderRepository.NewProductRepository(s.db)
	inventoryRepo := inventoryRepository.NewInventoryRepository(s.db)
	inventorySvc := inventoryService.NewInventoryService(s.validator, inventoryRepo)
	orderUOW := orderRepository.NewUnitOfWork(s.db, s.validator)

	return orderService.NewOrderService(s.validator, orderRepo, productRepo, inventorySvc, orderUOW)
}

func (s Server) newOutboxPublisher() (outboxService.EventPublisher, error) {
	switch publisherType := config.OutboxPublisherType(); publisherType {
	case config.OutboxPublisherTypeLog:
		return outboxService.NewLogPublisher(), nil
	case config.OutboxPublisherTypeRedisStream:
		return outboxService.NewRedisStreamPublisher(s.cache, config.OutboxRedisStreamName()), nil
	default:
		return nil, fmt.Errorf("unknown outbox publisher type %q", publisherType)
	}
}

func (s Server) startOutboxPublisher(parent context.Context) (backgroundStopFunc, error) {
	if !config.OutboxPublisherEnabled() {
		return noopBackgroundStop, nil
	}
	publisher, err := s.newOutboxPublisher()
	if err != nil {
		return noopBackgroundStop, err
	}

	worker := outboxService.NewPublisherWorker(
		outboxRepository.NewTransactor(s.db),
		publisher,
		outboxService.WithPublisherBatchSize(config.OutboxPublishBatchSize()),
		outboxService.WithPublisherMaxAttempts(config.OutboxPublishMaxAttempts()),
		outboxService.WithPublisherRetryBase(config.OutboxPublishRetryBase()),
		outboxService.WithPublisherProcessingTimeout(config.OutboxProcessingTimeout()),
	)
	ctx, cancel := context.WithCancel(parent)
	done := runOutboxPublisherLoop(ctx, worker, config.OutboxPublishInterval())

	return func() error {
		cancel()
		return waitBackgroundDone(done)
	}, nil
}

func (s Server) newOutboxConsumer() *outboxConsumer.RedisConsumer {
	return outboxConsumer.NewRedisConsumer(
		s.cache,
		outboxConsumer.NewLogEventHandler(),
		outboxConsumer.NewRedisProcessedEventStore(s.cache, config.OutboxConsumerProcessedTTL()),
		outboxConsumer.WithStreamName(config.OutboxRedisStreamName()),
		outboxConsumer.WithGroupName(config.OutboxConsumerGroup()),
		outboxConsumer.WithConsumerName(config.OutboxConsumerName()),
		outboxConsumer.WithBatchSize(int64(config.OutboxConsumerBatchSize())),
		outboxConsumer.WithBlock(config.OutboxConsumerBlock()),
		outboxConsumer.WithClaimMinIdle(config.OutboxConsumerClaimMinIdle()),
		outboxConsumer.WithClaimBatchSize(int64(config.OutboxConsumerClaimBatchSize())),
		outboxConsumer.WithMaxDeliveryAttempts(int64(config.OutboxConsumerMaxDeliveryAttempts())),
		outboxConsumer.WithFailureTTL(config.OutboxConsumerFailureTTL()),
		outboxConsumer.WithDeadLetterStreamName(config.OutboxDeadLetterStreamName()),
	)
}

func (s Server) startOutboxConsumer(parent context.Context) (backgroundStopFunc, error) {
	if !config.OutboxConsumerEnabled() {
		return noopBackgroundStop, nil
	}

	ctx, cancel := context.WithCancel(parent)
	consumer := s.newOutboxConsumer()
	if err := consumer.EnsureGroup(ctx); err != nil {
		cancel()
		return noopBackgroundStop, err
	}

	done := runOutboxConsumerLoop(ctx, consumer)
	return func() error {
		cancel()
		return waitBackgroundDone(done)
	}, nil
}

func runOutboxPublisherLoop(ctx context.Context, worker publisherRunner, interval time.Duration) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			result, err := worker.RunOnce(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logger.Error("outbox_publisher_batch_failed", err)
			} else if result.Fetched > 0 {
				logger.Info(
					"outbox_publisher_batch_complete fetched=", result.Fetched,
					" published=", result.Published,
					" failed=", result.Failed,
					" dead_lettered=", result.DeadLettered,
					" latency_ms=", result.Latency.Milliseconds(),
				)
			}

			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	return done
}

func runOutboxConsumerLoop(ctx context.Context, consumer consumerRunner) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			result, err := consumer.RunOnce(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logger.Error("outbox_consumer_batch_failed", err)
			} else if result.Read > 0 {
				logger.Info(
					"outbox_consumer_batch_complete read=", result.Read,
					" processed=", result.Processed,
					" skipped=", result.Skipped,
					" failed=", result.Failed,
					" invalid=", result.Invalid,
					" dead_lettered=", result.DeadLettered,
					" acked=", result.Acked,
				)
			}

			claimResult, err := consumer.ClaimStaleOnce(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				logger.Error("outbox_consumer_claim_failed", err)
			} else if claimResult.Read > 0 {
				logger.Info(
					"outbox_consumer_claim_complete read=", claimResult.Read,
					" processed=", claimResult.Processed,
					" skipped=", claimResult.Skipped,
					" failed=", claimResult.Failed,
					" invalid=", claimResult.Invalid,
					" dead_lettered=", claimResult.DeadLettered,
					" acked=", claimResult.Acked,
				)
			}
		}
	}()

	return done
}

func noopBackgroundStop() error {
	return nil
}

func waitBackgroundDone(done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-time.After(10 * time.Second):
		return errors.New("background worker did not stop before timeout")
	}
}
