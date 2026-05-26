package http

import (
	"github.com/gin-gonic/gin"

	"goshop/pkg/middleware"
)

func Routes(r *gin.RouterGroup, orderService orderService) {
	orderHandler := NewOrderHandler(orderService)
	authMiddleware := middleware.JWTAuth()

	orderRoute := r.Group("/orders", authMiddleware)
	{
		orderRoute.POST("", orderHandler.PlaceOrder)
		orderRoute.GET("/:id", orderHandler.GetOrderByID)
		orderRoute.GET("", orderHandler.GetOrders)
		orderRoute.PUT("/:id/cancel", orderHandler.CancelOrder)
	}
}
