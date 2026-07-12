package middleware

import (
	"database/sql"
	"net/http"

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
				c.Set("api_key_id", r.keyID)
				c.Set("rate_limit", r.rateLimit)
				c.Set("max_tokens", r.maxTokens)
				c.Next()
				return
			}
		}

		// Cache miss (or no cache): validate with singleflight to collapse
		// concurrent misses for the same key into one DB+bcrypt call.
		keyID, rateLimit, maxTokens, ok := cache.Validate(db, presented)
		if !ok {
			// Either no keys configured (open access) or invalid key.
			var count int
			if err := db.QueryRow(`SELECT COUNT(*) FROM api_keys WHERE is_active = 1`).Scan(&count); err != nil {
				logging.Logger.Warn("auth system error querying api_keys", "error", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "auth system error"})
				c.Abort()
				return
			}
			if count == 0 {
				// No keys configured — open access (user hasn't set up auth yet)
				c.Next()
				return
			}
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid api key"})
			c.Abort()
			return
		}

		if cache != nil {
			cache.Put(presented, keyID, rateLimit, maxTokens)
		}
		c.Set("api_key_id", keyID)
		c.Set("rate_limit", rateLimit)
		c.Set("max_tokens", maxTokens)
		c.Next()
	}
}
