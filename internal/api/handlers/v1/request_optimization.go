package v1

import (
	"database/sql"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/cache"
	"github.com/rickicode/AxonRouter-Go/internal/compression"
)

// compressRequestBody applies compression when enabled and safe. It is always
// fail-open: on error or when the request contains prompt-cache markers the
// original body is returned. Compression stats are recorded for the metrics
// endpoint asynchronously and will not block the request.
func (h *Handler) compressRequestBody(body []byte) []byte {
	if h.compressionStrategy.Mode == compression.ModeOff || compression.HasCacheControl(body) {
		return body
	}
	compressed, stats, err := compression.Apply(h.compressionStrategy, body)
	if err != nil {
		return body
	}
	h.recordCompressionMetrics(stats)
	return compressed
}

// recordCompressionMetrics persists aggregated compression stats for the active
// mode. It is best-effort: failures are logged but never block the request.
func (h *Handler) recordCompressionMetrics(stats compression.EngineStats) {
	mode := string(h.compressionStrategy.Mode)
	if mode == "" || mode == string(compression.ModeOff) {
		return
	}
	if stats.OriginalTokens == 0 && stats.CompressedTokens == 0 {
		return
	}
	now := time.Now().Unix()
	upsert := func(d *sql.DB) error {
		_, err := d.Exec(`INSERT INTO compression_metrics
			(mode, requests, original_tokens, compressed_tokens, updated_at)
			VALUES (?, 1, ?, ?, ?)
			ON CONFLICT(mode) DO UPDATE SET
				requests = requests + 1,
				original_tokens = original_tokens + excluded.original_tokens,
				compressed_tokens = compressed_tokens + excluded.compressed_tokens,
				updated_at = excluded.updated_at`, mode, stats.OriginalTokens, stats.CompressedTokens, now)
		return err
	}
	if h.writeQueue != nil {
		h.writeQueue.Enqueue("compression:recordMetrics", upsert)
	} else {
		// Fallback for tests/legacy setups without a write queue.
		_ = upsert(h.db)
	}
}

// exactCacheKey returns a cache key for exact-match non-streaming requests
// without tools or cache_control markers. An empty string means the request
// should not be cached.
func (h *Handler) exactCacheKey(body []byte, model string, stream bool) string {
	if stream || h.exactCache == nil || compression.HasTools(body) || compression.HasCacheControl(body) {
		return ""
	}
	return cache.ComputeKey(body, model)
}

// serveCacheHit writes a cached response to the client and returns true.
func (h *Handler) serveCacheHit(c *gin.Context, entry cache.CacheEntry) bool {
	c.Header("Content-Type", entry.ContentType)
	c.Header("X-Cache-Status", "HIT")
	c.Status(entry.StatusCode)
	c.Writer.Write(entry.Body)
	return true
}

// storeExactCache persists a successful non-streaming response when a cache key
// was computed.
func (h *Handler) storeExactCache(cacheKey string, body []byte, statusCode int) {
	if cacheKey == "" || h.exactCache == nil {
		return
	}
	h.exactCache.Set(cacheKey, cache.CacheEntry{
		Body:        body,
		StatusCode:  statusCode,
		ContentType: "application/json",
	})
}

// writeJSONResponse writes a JSON response and marks it as a cache miss.
func (h *Handler) writeJSONResponse(c *gin.Context, statusCode int, body []byte) {
	c.Header("Content-Type", "application/json")
	c.Header("X-Cache-Status", "MISS")
	c.Status(statusCode)
	c.Writer.Write(body)
}
