package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

// Logging logs HTTP requests in a compact format.
func Logging() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		clientIP := c.ClientIP()
		method := c.Request.Method

		if query != "" {
			path = path + "?" + query
		}

		logging.Logger.Info("http request",
			"request_id", c.GetString("request_id"),
			"method", method,
			"path", path,
			"status", status,
			"latency", latency,
			"client_ip", clientIP,
		)
	}
}
