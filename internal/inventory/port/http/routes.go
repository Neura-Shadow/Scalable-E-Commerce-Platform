package http

import (
	"github.com/gin-gonic/gin"
	"github.com/quangdangfit/gocommon/validation"

	"goshop/internal/inventory/repository"
	"goshop/internal/inventory/service"
	"goshop/pkg/dbs"
	"goshop/pkg/middleware"
)

func Routes(r *gin.RouterGroup, db dbs.IDatabase, validator validation.Validation) {
	inventoryRepo := repository.NewInventoryRepository(db)
	inventorySvc := service.NewInventoryService(validator, inventoryRepo)
	inventoryHandler := NewInventoryHandler(inventorySvc)

	authMiddleware := middleware.JWTAuth()
	adminOnly := middleware.RequireRole("admin")

	inventoryRoute := r.Group("/inventory")
	{
		inventoryRoute.GET("", inventoryHandler.List)
		inventoryRoute.GET("/:product_id", inventoryHandler.Get)
		inventoryRoute.PUT("/:product_id", authMiddleware, adminOnly, inventoryHandler.Set)
		inventoryRoute.PATCH("/:product_id/adjust", authMiddleware, adminOnly, inventoryHandler.Adjust)
	}
}
