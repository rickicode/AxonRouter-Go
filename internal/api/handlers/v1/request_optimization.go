package v1

import (
	"database/sql"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/cache"
	"github.com/rickicode/AxonRouter-Go/internal/compression"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
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

// serveCacheHit writes a cached response to the client and accounts for the
// request tokens against the API key budget before returning.
func (h *Handler) serveCacheHit(c *gin.Context, body []byte, entry cache.CacheEntry) bool {
	h.incrementAPIKeyUsage(c.GetString("api_key_id"), usage.EstimateTokensFromRequest(body))

	cachedModel := executor.JSONGet(entry.Body, "model")
	counts := ExtractTokensFromBody(entry.Body)
	provider, _ := executor.SplitModel(cachedModel)
	writeCostHeaders(c, cachedModel, 0, counts, false, h.isFlatRate(provider))

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

// responseCost carries optional per-response costing metadata so that every
// proxied JSON response can expose the same values that are persisted in
// request_logs.cost_usd.
type responseCost struct {
	modelID         string
	exactCost       float64
	counts          StreamTokenCounts
	tokensEstimated bool
	flatRate        bool
}

// writeJSONResponse writes a JSON response, marks it as a cache miss, and
// attaches the AxonRouter cost headers when cost metadata is supplied.
func (h *Handler) writeJSONResponse(c *gin.Context, statusCode int, body []byte, cost ...responseCost) {
	c.Header("Content-Type", "application/json")
	c.Header("X-Cache-Status", "MISS")
	if len(cost) > 0 {
		writeCostHeaders(c, cost[0].modelID, cost[0].exactCost, cost[0].counts, cost[0].tokensEstimated, cost[0].flatRate)
	}
	c.Status(statusCode)
	c.Writer.Write(body)
}

const (
	costHeader          = "X-AxonRouter-Response-Cost"
	tokensInHeader      = "X-AxonRouter-Tokens-In"
	tokensOutHeader     = "X-AxonRouter-Tokens-Out"
	costEstimatedHeader = "X-AxonRouter-Cost-Estimated"
	costTrailerNames    = "X-AxonRouter-Response-Cost, X-AxonRouter-Tokens-In, X-AxonRouter-Tokens-Out, X-AxonRouter-Cost-Estimated"
)

// writeCostHeaders sets the standard cost-related response headers. exactCost
// should be the provider-reported cost (e.g., Grok CLI) when available; when it
// is zero the cost is estimated from the model pricing and token counts. When
// flatRate is true the cost is always reported as $0 so subscription/cookie-web
// providers do not inflate dashboard analytics, while request_logs.cost_usd
// continues to store the estimated cost for internal budget/quota tracking.
func writeCostHeaders(c *gin.Context, modelID string, exactCost float64, counts StreamTokenCounts, tokensEstimated, flatRate bool) {
	cost := 0.0
	if !flatRate {
		cost = exactCost
		if cost <= 0 {
			cost = usage.EstimateCost(modelID, counts.InputTokens, counts.OutputTokens, counts.ReasoningTokens, counts.CachedTokens, counts.CacheCreationTokens)
		}
	}

	c.Header(costHeader, strconv.FormatFloat(cost, 'f', -1, 64))
	c.Header(tokensInHeader, strconv.FormatInt(counts.InputTokens, 10))
	c.Header(tokensOutHeader, strconv.FormatInt(counts.OutputTokens, 10))

	estimated := "false"
	if !flatRate && exactCost <= 0 && (counts.InputTokens > 0 || counts.OutputTokens > 0 || tokensEstimated) {
		estimated = "true"
	}
	c.Header(costEstimatedHeader, estimated)
}

// writeCostTrailers declares and writes the cost trailers for streaming
// responses. Callers must invoke this after the SSE stream body has finished.
func writeCostTrailers(c *gin.Context, modelID string, exactCost float64, counts StreamTokenCounts, tokensEstimated, flatRate bool) {
	cost := 0.0
	if !flatRate {
		cost = exactCost
		if cost <= 0 {
			cost = usage.EstimateCost(modelID, counts.InputTokens, counts.OutputTokens, counts.ReasoningTokens, counts.CachedTokens, counts.CacheCreationTokens)
		}
	}

	c.Writer.Header().Set(costHeader, strconv.FormatFloat(cost, 'f', -1, 64))
	c.Writer.Header().Set(tokensInHeader, strconv.FormatInt(counts.InputTokens, 10))
	c.Writer.Header().Set(tokensOutHeader, strconv.FormatInt(counts.OutputTokens, 10))

	estimated := "false"
	if !flatRate && exactCost <= 0 && (counts.InputTokens > 0 || counts.OutputTokens > 0 || tokensEstimated) {
		estimated = "true"
	}
	c.Writer.Header().Set(costEstimatedHeader, estimated)
}
