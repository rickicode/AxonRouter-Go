package admin

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/active"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// LogHandler handles request log queries.
type LogHandler struct {
	db *sql.DB
}

// NewLogHandler creates a new log handler.
func NewLogHandler(database *sql.DB) *LogHandler {
	return &LogHandler{db: database}
}

// List returns paginated request logs with filters.
func (h *LogHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "100"))

	filter := parseLogFilter(c)

	result, err := usage.QueryLogs(h.db, page, perPage, filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// parseLogFilter maps HTTP query parameters to a usage.LogFilter using the
// query names expected by the dashboard UI (with simple legacy fallbacks).
func parseLogFilter(c *gin.Context) usage.LogFilter {
	filter := usage.LogFilter{
		ProviderTypeID: firstQuery(c, "provider_id", "provider_type_id"),
		ConnectionID:   firstQuery(c, "connection_id"),
		ModelID:        firstQuery(c, "model_id"),
		ComboID:        firstQuery(c, "combo_id"),
		Modality:       firstQuery(c, "modality"),
		StatusFilter:   firstQuery(c, "status"),
		Search:         firstQuery(c, "search"),
	}

	if codeStr := firstQuery(c, "status_code"); codeStr != "" {
		filter.StatusCode, _ = strconv.Atoi(codeStr)
	}

	// Date filters: UI sends ISO dates; legacy callers may send raw ms timestamps.
	if start := firstQuery(c, "start_date"); start != "" {
		t, _ := time.Parse(time.DateOnly, start)
		if !t.IsZero() {
			filter.Since = t.UnixMilli()
		}
	} else if since := firstQuery(c, "since"); since != "" {
		filter.Since, _ = strconv.ParseInt(since, 10, 64)
	}

	if end := firstQuery(c, "end_date"); end != "" {
		t, _ := time.Parse(time.DateOnly, end)
		if !t.IsZero() {
			filter.Until = t.Add(24*time.Hour - time.Millisecond).UnixMilli()
		}
	} else if until := firstQuery(c, "until"); until != "" {
		filter.Until, _ = strconv.ParseInt(until, 10, 64)
	}

	return filter
}

// firstQuery returns the first non-empty query value among the given keys.
func firstQuery(c *gin.Context, keys ...string) string {
	for _, k := range keys {
		if v := c.Query(k); v != "" {
			return v
		}
	}
	return ""
}

// ActiveRequests returns currently in-flight proxied requests for the
// live in-flight panel on the Logs page.
func (h *LogHandler) ActiveRequests(c *gin.Context) {
	c.JSON(http.StatusOK, active.List())
}

// Get returns a single log entry.
func (h *LogHandler) Get(c *gin.Context) {
	id := c.Param("id")
	var l struct {
		ID                  string  `json:"id"`
		Timestamp           int64   `json:"timestamp"`
		ConnectionID        *string `json:"connection_id"`
		ProviderTypeID      *string `json:"provider_type_id"`
		ModelID             *string `json:"model_id"`
		ComboID             *string `json:"combo_id"`
		Modality            string  `json:"modality"`
		InputTokens         int64   `json:"input_tokens"`
		OutputTokens        int64   `json:"output_tokens"`
		ReasoningTokens     int64   `json:"reasoning_tokens"`
		CachedTokens        int64   `json:"cached_tokens"`
	CacheCreationTokens int64  `json:"cache_creation_tokens"`
	TokensEstimated     bool   `json:"tokens_estimated"`
		LatencyMs           *int64  `json:"latency_ms"`
		StatusCode          *int64  `json:"status_code"`
  ErrorMessage *string `json:"error_message"`
  CostUsd float64 `json:"cost_usd"`
  ClientIP *string `json:"client_ip"`
  UserAgent *string `json:"user_agent"`
  CreatedAt int64 `json:"created_at"`
 }

		err := h.db.QueryRow(`
		SELECT id, timestamp, connection_id, provider_type_id, model_id, combo_id,
		modality, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_creation_tokens,
  tokens_estimated,
  latency_ms, status_code, error_message, cost_usd, client_ip, user_agent, created_at
  FROM request_logs WHERE id = ?
 `, id).Scan(&l.ID, &l.Timestamp, &l.ConnectionID, &l.ProviderTypeID,
  &l.ModelID, &l.ComboID, &l.Modality,
  &l.InputTokens, &l.OutputTokens, &l.ReasoningTokens, &l.CachedTokens, &l.CacheCreationTokens,
  &l.TokensEstimated,
  &l.LatencyMs, &l.StatusCode, &l.ErrorMessage,
  &l.CostUsd, &l.ClientIP, &l.UserAgent, &l.CreatedAt)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "log not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, l)
}

// Stats returns aggregated log statistics.
func (h *LogHandler) Stats(c *gin.Context) {
	hours, _ := strconv.Atoi(c.DefaultQuery("hours", "24"))
	agg := usage.NewAggregator(h.db)

	providerUsage, _ := agg.GetProviderUsage(hours)
	modelUsage, _ := agg.GetModelUsage(hours)
	dailyUsage, _ := agg.GetDailyUsage(30)

	c.JSON(http.StatusOK, gin.H{
		"provider_usage": providerUsage,
		"model_usage":    modelUsage,
		"daily_usage":    dailyUsage,
	})
}
