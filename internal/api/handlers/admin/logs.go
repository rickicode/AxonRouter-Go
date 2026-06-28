package admin

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
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

	filter := usage.LogFilter{
		ProviderTypeID: c.Query("provider_type_id"),
		ConnectionID:   c.Query("connection_id"),
		ModelID:        c.Query("model_id"),
		ComboID:        c.Query("combo_id"),
		Modality:       c.Query("modality"),
		StatusFilter:   c.Query("status"),
		Search:         c.Query("search"),
	}

	if since := c.Query("since"); since != "" {
		if ts, err := strconv.ParseInt(since, 10, 64); err == nil {
			filter.Since = ts
		}
	}
	if until := c.Query("until"); until != "" {
		if ts, err := strconv.ParseInt(until, 10, 64); err == nil {
			filter.Until = ts
		}
	}

	result, err := usage.QueryLogs(h.db, page, perPage, filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// Get returns a single log entry.
func (h *LogHandler) Get(c *gin.Context) {
	id := c.Param("id")
	var l struct {
		ID              string  `json:"id"`
		Timestamp       int64   `json:"timestamp"`
		ConnectionID    *string `json:"connection_id"`
		ProviderTypeID  *string `json:"provider_type_id"`
		ModelID         *string `json:"model_id"`
		ComboID         *string `json:"combo_id"`
		Modality        string  `json:"modality"`
		InputTokens     int64   `json:"input_tokens"`
		OutputTokens    int64   `json:"output_tokens"`
		ReasoningTokens int64   `json:"reasoning_tokens"`
		LatencyMs       *int64  `json:"latency_ms"`
		StatusCode      *int64  `json:"status_code"`
		ErrorMessage    *string `json:"error_message"`
		CostUsd         float64 `json:"cost_usd"`
		CreatedAt       int64   `json:"created_at"`
	}

	err := h.db.QueryRow(`
		SELECT id, timestamp, connection_id, provider_type_id, model_id, combo_id,
		       modality, input_tokens, output_tokens, reasoning_tokens,
		       latency_ms, status_code, error_message, cost_usd, created_at
		FROM request_logs WHERE id = ?
	`, id).Scan(&l.ID, &l.Timestamp, &l.ConnectionID, &l.ProviderTypeID,
		&l.ModelID, &l.ComboID, &l.Modality,
		&l.InputTokens, &l.OutputTokens, &l.ReasoningTokens,
		&l.LatencyMs, &l.StatusCode, &l.ErrorMessage,
		&l.CostUsd, &l.CreatedAt)
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
