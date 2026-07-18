package middleware

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

// skipExt are static-asset extensions that clutter logs.
var skipExt = []string{".png", ".svg", ".ico", ".css", ".js", ".woff", ".woff2", ".ttf", ".eot", ".map"}

// Logging logs HTTP requests in a compact format, skipping static assets.
func Logging() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		// Skip static assets
		for _, ext := range skipExt {
			if strings.HasSuffix(path, ext) {
				c.Next()
				return
			}
		}

		query := c.Request.URL.RawQuery
		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		method := c.Request.Method

		fullPath := path
		if query != "" {
			fullPath = path + "?" + query
		}

		// Compact format: [STATUS] METHOD /path 123ms
		latStr := formatLatency(latency)
		logging.Logger.Info("http",
			"status", status,
			"method", method,
			"path", fullPath,
			"lat", latStr,
			"client_ip", c.ClientIP(),
			"user_agent", c.Request.UserAgent(),
		)
	}
}

func formatLatency(d time.Duration) string {
	if d < time.Millisecond {
		return d.Round(time.Microsecond).String()
	}
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(10 * time.Millisecond).String()
}
