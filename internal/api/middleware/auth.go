package middleware

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

// Auth validates API keys from the Authorization header.
// Fail-closed: returns 401 when no keys are configured.
func Auth(db *sql.DB, cache *AuthCache) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Resolve the presented key once.
		authHeader := c.GetHeader("Authorization")
		presented := authHeader
		if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			presented = authHeader[7:]
		}

	// Cache hit: zero DB access, zero bcrypt. This is the hot path.
	if presented != "" && cache != nil {
		if r := cache.Get(presented); r != nil {
			if r.expiresAt > 0 && time.Now().Unix() >= r.expiresAt {
				cache.Invalidate(presented)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "api key expired"})
				return
			}
			c.Set("api_key_id", r.keyID)
			c.Set("rate_limit", r.rateLimit)
			c.Set("max_tokens", r.maxTokens)
			c.Next()
			return
		}
	}

	// Cache miss (or no cache): validate with singleflight to collapse
	// concurrent misses for the same key into one DB+bcrypt call.
	keyID, rateLimit, maxTokens, ok, expired, dbErr := cache.Validate(db, presented)
	if dbErr != nil {
		logging.Logger.Warn("auth system error querying api_keys", "error", dbErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "auth system error"})
		c.Abort()
		return
	}
	if expired {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "api key expired"})
		return
	}
	if !ok {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM api_keys WHERE is_active = 1`).Scan(&count); err != nil {
			logging.Logger.Warn("auth system error querying api_keys", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "auth system error"})
			c.Abort()
			return
		}
		if count == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "no api keys configured"})
			return
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
		return
	}

	c.Set("api_key_id", keyID)
	c.Set("rate_limit", rateLimit)
	c.Set("max_tokens", maxTokens)
	c.Next()
}
}
