package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimiter is a simple in-memory rate limiter with per-key support.
type RateLimiter struct {
	mu          sync.Mutex
	buckets     map[string]*tokenBucket
	globalLimit int
	window      time.Duration
}

type tokenBucket struct {
	tokens    int
	maxTokens int
	lastTime  time.Time
}

// NewRateLimiter creates a rate limiter with the given requests per minute.
func NewRateLimiter(requestsPerMin int) *RateLimiter {
	rl := &RateLimiter{
		buckets:     make(map[string]*tokenBucket),
		globalLimit: requestsPerMin,
		window:      time.Minute,
	}
	go rl.cleanup()
	return rl
}

// RateLimit returns a Gin middleware that rate-limits by IP or per-key.
func RateLimit(limiter *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if limiter == nil {
			c.Next()
			return
		}

		// Use per-key limit if available, fall back to global
		key := c.ClientIP()
		limit := 0
		if v, exists := c.Get("rate_limit"); exists {
			if l, ok := v.(int); ok && l > 0 {
				limit = l
				// Use stable API key ID as bucket key; hash the header if no ID is set
				if id, exists := c.Get("api_key_id"); exists && id != "" {
					if s, ok := id.(string); ok {
						key = s
					}
				} else if authKey := c.GetHeader("Authorization"); authKey != "" {
					sum := sha256.Sum256([]byte(authKey))
					key = hex.EncodeToString(sum[:])
				}
			}
		}

		if !limiter.Allow(key, limit) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{
				"message": "rate limit exceeded",
				"type":    "rate_limit_error",
			}})
			c.Abort()
			return
		}
		c.Next()
	}
}

// Allow checks if a request is allowed. If perKeyLimit > 0, uses it; otherwise uses global limit.
func (rl *RateLimiter) Allow(key string, perKeyLimit int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, exists := rl.buckets[key]
	if !exists {
		limit := rl.globalLimit
		if perKeyLimit > 0 {
			limit = perKeyLimit
		}
		bucket = &tokenBucket{
			tokens:    limit,
			maxTokens: limit,
			lastTime:  time.Now(),
		}
		rl.buckets[key] = bucket
	} else if perKeyLimit > 0 && bucket.maxTokens != perKeyLimit {
		// Update maxTokens if perKeyLimit changed (DB rate_limit update)
		bucket.maxTokens = perKeyLimit
		if bucket.tokens > perKeyLimit {
			bucket.tokens = perKeyLimit
		}
	}

	now := time.Now()
	elapsed := now.Sub(bucket.lastTime)
	bucket.tokens += int(elapsed.Seconds()) * (bucket.maxTokens / 60)
	if bucket.tokens > bucket.maxTokens {
		bucket.tokens = bucket.maxTokens
	}
	bucket.lastTime = now

	if bucket.tokens <= 0 {
		return false
	}
	bucket.tokens--
	return true
}

// cleanup periodically removes stale buckets.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		for k, b := range rl.buckets {
			if time.Since(b.lastTime) > 10*time.Minute {
				delete(rl.buckets, k)
			}
		}
		rl.mu.Unlock()
	}
}
