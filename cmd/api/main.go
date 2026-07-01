package main

import (
	"github.com/quangdangfit/gocommon/logger"
	"github.com/quangdangfit/gocommon/validation"

	inventoryModel "goshop/internal/inventory/model"
	orderModel "goshop/internal/order/model"
	outboxModel "goshop/internal/outbox/model"
	productModel "goshop/internal/product/model"
	grpcServer "goshop/internal/server/grpc"
	httpServer "goshop/internal/server/http"
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

	db, err := dbs.NewDatabase(cfg.DatabaseURI)
	if err != nil {
		logger.Fatal("Cannot connect to database", err)
	}

	err = db.AutoMigrate(&userModel.User{}, &productModel.Product{}, &inventoryModel.Inventory{}, orderModel.Order{}, orderModel.OrderLine{}, &outboxModel.OutboxEvent{})
	if err != nil {
		logger.Fatal("Database migration fail", err)
	}

	validator := validation.New()

	cache := redis.New(redis.Config{
		Address:  cfg.RedisURI,
		Password: cfg.RedisPassword,
		Database: cfg.RedisDB,
	})

	go func() {
		httpSvr := httpServer.NewServer(validator, db, cache)
		if err = httpSvr.Run(); err != nil {
			logger.Fatal(err)
		}
	}()

	grpcSvr := grpcServer.NewServer(validator, db, cache)
	if err = grpcSvr.Run(); err != nil {
		logger.Fatal(err)
	}
}
