package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
)

// RequestID ensures every request has a unique request_id attached to both
// the Gin context and the underlying request context. The ID is read from the
// X-Request-ID header if present, otherwise a new UUID is generated.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		c.Set("request_id", id)
		c.Request = c.Request.WithContext(executor.ContextWithRequestID(c.Request.Context(), id))
		c.Next()
	}
}
