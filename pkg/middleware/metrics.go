package middleware

import (
	"time"

	"github.com/gin-gonic/gin"

	appMetrics "goshop/pkg/metrics"
)

func HTTPMetrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()

		path := c.FullPath()
		if path == "" {
			path = "unmatched"
		}
		appMetrics.RecordHTTPStatus(c.Request.Method, path, c.Writer.Status(), time.Since(startedAt))
	}
}
