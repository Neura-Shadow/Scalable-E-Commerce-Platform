package http

import (
	"github.com/gin-gonic/gin"
	"github.com/quangdangfit/gocommon/validation"

	"goshop/internal/product/repository"
	"goshop/internal/product/service"
	"goshop/pkg/dbs"
	"goshop/pkg/middleware"
	"goshop/pkg/redis"
)

func Routes(r *gin.RouterGroup, db dbs.IDatabase, validator validation.Validation, cache redis.IRedis) {
	productRepo := repository.NewProductRepository(db)
	productSvc := service.NewProductService(validator, productRepo)
	productHandler := NewProductHandler(cache, productSvc)

	authMiddleware := middleware.JWTAuth()
	adminOnly := middleware.RequireRole("admin")

	productRoute := r.Group("/products")
	{
		productRoute.GET("", productHandler.ListProducts)
		productRoute.POST("", authMiddleware, adminOnly, productHandler.CreateProduct)
		productRoute.PUT("/:id", authMiddleware, adminOnly, productHandler.UpdateProduct)
		productRoute.GET("/:id", productHandler.GetProductByID)
	}
}
