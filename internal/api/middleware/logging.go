package middleware

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
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

		// ponytail: standard log format — upgrade to structured logging later
		log.Printf("[%s] %3d | %13v | %15s | %s %s",
			time.Now().Format("15:04:05"),
			status,
			latency,
			clientIP,
			method,
			path,
		)
	}
}
