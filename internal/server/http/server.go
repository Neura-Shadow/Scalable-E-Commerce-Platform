package http

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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

func (s Server) Run() error {
	if s.cfg.Environment == config.ProductionEnv {
		gin.SetMode(gin.ReleaseMode)
	}

	if err := s.MapRoutes(); err != nil {
		log.Fatalf("MapRoutes Error: %v", err)
	}
	s.engine.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	stopOutboxPublisher, err := s.startOutboxPublisher()
	if err != nil {
		return err
	}
	defer stopOutboxPublisher()
	stopOutboxConsumer, err := s.startOutboxConsumer()
	if err != nil {
		return err
	}
	defer stopOutboxConsumer()

	// Start http server
	logger.Info("HTTP server is listening on PORT: ", s.cfg.HttpPort)
	httpServer := s.newHTTPServer()
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Running HTTP server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		return err
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

func (s Server) startOutboxPublisher() (context.CancelFunc, error) {
	ctx, cancel := context.WithCancel(context.Background())
	publisher, err := s.newOutboxPublisher()
	if err != nil {
		return cancel, err
	}
	if !config.OutboxPublisherEnabled() {
		return cancel, nil
	}

	worker := outboxService.NewPublisherWorker(
		outboxRepository.NewTransactor(s.db),
		publisher,
		outboxService.WithPublisherBatchSize(config.OutboxPublishBatchSize()),
		outboxService.WithPublisherMaxAttempts(config.OutboxPublishMaxAttempts()),
		outboxService.WithPublisherRetryBase(config.OutboxPublishRetryBase()),
	)
	interval := config.OutboxPublishInterval()
	ticker := time.NewTicker(interval)

	go func() {
		defer ticker.Stop()
		for {
			result, err := worker.RunOnce(ctx)
			if err != nil {
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

	return cancel, nil
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

func (s Server) startOutboxConsumer() (context.CancelFunc, error) {
	ctx, cancel := context.WithCancel(context.Background())
	if !config.OutboxConsumerEnabled() {
		return cancel, nil
	}

	consumer := s.newOutboxConsumer()
	if err := consumer.EnsureGroup(ctx); err != nil {
		cancel()
		return cancel, err
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			result, err := consumer.RunOnce(ctx)
			if err != nil {
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

	return cancel, nil
}
