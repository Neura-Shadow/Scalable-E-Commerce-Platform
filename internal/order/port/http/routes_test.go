package http

import (
	"testing"

	"github.com/gin-gonic/gin"

	"goshop/internal/order/service/mocks"
)

func TestRoutes(t *testing.T) {
	mockService := mocks.NewIOrderService(t)
	Routes(gin.New().Group("/"), mockService)
}
