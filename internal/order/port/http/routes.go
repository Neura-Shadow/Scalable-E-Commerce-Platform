package http

import (
	"github.com/gin-gonic/gin"

	"goshop/pkg/middleware"
	"goshop/pkg/redis"
)

func Routes(r *gin.RouterGroup, orderService orderService, caches ...redis.IRedis) {
	var opts []OrderHandlerOption
	if len(caches) > 0 && caches[0] != nil {
		opts = append(opts, WithOrderCache(caches[0]))
	}
	orderHandler := NewOrderHandler(orderService, opts...)
	authMiddleware := middleware.JWTAuth()

	orderRoute := r.Group("/orders", authMiddleware)
	{
		orderRoute.POST("", orderHandler.PlaceOrder)
		orderRoute.GET("/:id", orderHandler.GetOrderByID)
		orderRoute.GET("", orderHandler.GetOrders)
		orderRoute.PUT("/:id/cancel", orderHandler.CancelOrder)
	}
}
