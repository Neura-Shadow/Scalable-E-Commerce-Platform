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
	productHttp "goshop/internal/product/port/http"
	userHttp "goshop/internal/user/port/http"
	"goshop/pkg/config"
	"goshop/pkg/dbs"
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
