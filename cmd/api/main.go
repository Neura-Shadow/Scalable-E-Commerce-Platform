package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/quangdangfit/gocommon/logger"
	"github.com/quangdangfit/gocommon/validation"

	cartModel "goshop/internal/cart/model"
	inventoryModel "goshop/internal/inventory/model"
	orderModel "goshop/internal/order/model"
	outboxModel "goshop/internal/outbox/model"
	productModel "goshop/internal/product/model"
	grpcServer "goshop/internal/server/grpc"
	httpServer "goshop/internal/server/http"
	"goshop/internal/server/lifecycle"
	userModel "goshop/internal/user/model"
	"goshop/pkg/config"
	"goshop/pkg/dbs"
	"goshop/pkg/redis"
)

//	@title			Scalable E-Commerce Platform API
//	@version		1.0
//	@description	REST API for a production-minded Go e-commerce backend with transaction-safe orders, atomic inventory deduction, idempotent order creation, and admin/customer permissions.
//	@termsOfService	http://swagger.io/terms/

//	@contact.name	Neura Shadow

//	@license.name	MIT
//	@license.url	https://github.com/Neura-Shadow/Scalable-E-Commerce-Platform

//	@securityDefinitions.apikey	ApiKeyAuth
//	@in							header
//	@name						Authorization

//	@BasePath	/api/v1

func main() {
	cfg := config.LoadConfig()
	logger.Initialize(cfg.Environment)

	db, err := dbs.NewDatabase(dbs.Config{
		URI:             cfg.DatabaseURI,
		MaxOpenConns:    config.DatabaseMaxOpenConns(),
		MaxIdleConns:    config.DatabaseMaxIdleConns(),
		ConnMaxLifetime: config.DatabaseConnMaxLifetime(),
		ConnMaxIdleTime: config.DatabaseConnMaxIdleTime(),
	})
	if err != nil {
		logger.Fatal("Cannot connect to database", err)
	}

	if err = migrateDatabase(db, config.DatabaseAutoMigrate()); err != nil {
		logger.Fatal("Database migration fail", err)
	}

	validator := validation.New()

	cache := redis.New(redis.Config{
		Address:  cfg.RedisURI,
		Password: cfg.RedisPassword,
		Database: cfg.RedisDB,
	})

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	httpSvr := httpServer.NewServer(validator, db, cache)
	grpcSvr := grpcServer.NewServer(validator, db, cache)

	if err = lifecycle.Run(rootCtx, httpSvr, grpcSvr); err != nil {
		logger.Fatal(err)
	}
}

func migrateDatabase(db dbs.IDatabase, enabled bool) error {
	if !enabled {
		return nil
	}

	return db.AutoMigrate(
		&userModel.User{},
		&productModel.Product{},
		&inventoryModel.Inventory{},
		orderModel.Order{},
		orderModel.OrderLine{},
		&outboxModel.OutboxEvent{},
		&cartModel.Cart{},
		&cartModel.CartLine{},
	)
}
