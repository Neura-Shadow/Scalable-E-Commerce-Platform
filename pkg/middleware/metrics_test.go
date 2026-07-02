package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appMetrics "goshop/pkg/metrics"
)

func TestHTTPMetricsUsesNormalizedRoutePath(t *testing.T) {
	appMetrics.ResetForTest()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(HTTPMetrics())
	router.GET("/api/v1/orders/:id", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/orders/order-123", nil)
	writer := httptest.NewRecorder()
	router.ServeHTTP(writer, request)

	snapshot, err := appMetrics.SnapshotText()

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, writer.Code)
	assert.Contains(t, snapshot, "http_requests_total")
	assert.Contains(t, snapshot, `path="/api/v1/orders/:id"`)
	assert.NotContains(t, snapshot, "order-123")
}
