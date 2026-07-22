package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

// allowedModelsCtxKey is the private key for storing/retrieving the allowed
// model set on a request context.
type allowedModelsCtxKey struct{}

// contextWithAllowedModels attaches the allowed model set to ctx.
func contextWithAllowedModels(ctx context.Context, allowed map[string]struct{}) context.Context {
	return context.WithValue(ctx, allowedModelsCtxKey{}, allowed)
}

// AllowedModelsFromContext returns the allowed model set previously attached
// to ctx by the auth middleware. A nil/empty set means all models are allowed.
func AllowedModelsFromContext(ctx context.Context) map[string]struct{} {
	if v, ok := ctx.Value(allowedModelsCtxKey{}).(map[string]struct{}); ok {
		return v
	}
	return nil
}

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
				c.Set("allowed_models", r.allowedModels)
				c.Request = c.Request.WithContext(contextWithAllowedModels(c.Request.Context(), r.allowedModels))
				c.Next()
				return
			}
		}

		// Cache miss (or no cache): validate with singleflight to collapse
		// concurrent misses for the same key into one DB+bcrypt call.
		keyID, rateLimit, maxTokens, allowedModels, ok, expired, dbErr := cache.Validate(db, presented)
		if dbErr != nil {
			logging.Logger.Warn("auth system error querying api_keys", "error", dbErr)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "auth system error"})
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
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "auth system error"})
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
		c.Set("allowed_models", allowedModels)
		c.Request = c.Request.WithContext(contextWithAllowedModels(c.Request.Context(), allowedModels))
		c.Next()
	}
}
