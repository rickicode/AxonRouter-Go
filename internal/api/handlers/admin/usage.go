package admin

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// UsageHandler aggregates request_logs into usage reports per API key, model,
// provider, and time bucket.
type UsageHandler struct {
	db *sql.DB
}

// NewUsageHandler creates a new usage handler.
func NewUsageHandler(db *sql.DB) *UsageHandler {
	return &UsageHandler{db: db}
}

type usageBreakdown struct {
	APIKeyID     string  `json:"api_key_id,omitempty"`
	APIKeyName   string  `json:"api_key_name,omitempty"`
	ModelID      string  `json:"model_id,omitempty"`
	ProviderID   string  `json:"provider_id,omitempty"`
	ProviderName string  `json:"provider_name,omitempty"`
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	ReasonTokens int64   `json:"reasoning_tokens"`
	TotalTokens  int64   `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

type timeBucket struct {
	Bucket   string  `json:"bucket"`
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	CostUSD  float64 `json:"cost_usd"`
}

type usageSummary struct {
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	ReasonTokens int64   `json:"reasoning_tokens"`
	TotalTokens  int64   `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

type usageResponse struct {
	Summary    usageSummary     `json:"summary"`
	ByAPIKey   []usageBreakdown `json:"by_api_key"`
	ByModel    []usageBreakdown `json:"by_model"`
	ByProvider []usageBreakdown `json:"by_provider"`
	ByTime     []timeBucket     `json:"by_time"`
}

// Get aggregates usage across all API keys. Query params:
//   - from/to: ISO dates (YYYY-MM-DD), default to last 30 days.
//   - granularity: "day" or "month" (default "day").
func (h *UsageHandler) Get(c *gin.Context) {
	now := time.Now().UTC()
	defaultFrom := now.AddDate(0, 0, -30).Truncate(24 * time.Hour)

	from := parseDate(c.Query("from"), defaultFrom)
	to := parseDate(c.Query("to"), now.Truncate(24*time.Hour)).Add(24*time.Hour - time.Second)
	granularity := c.DefaultQuery("granularity", "day")
	if granularity != "day" && granularity != "month" {
		granularity = "day"
	}

	if granularity == "month" {
		from = time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
		to = time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0).Add(-time.Second)
	}

	fromUnix := from.Unix()
	toUnix := to.Unix()

	ctx := c.Request.Context()
	summary, err := h.summary(ctx, fromUnix, toUnix)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	byAPIKey, err := h.byAPIKey(ctx, fromUnix, toUnix)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	byModel, err := h.byModel(ctx, fromUnix, toUnix)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	byProvider, err := h.byProvider(ctx, fromUnix, toUnix)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	byTime, err := h.byTime(ctx, fromUnix, toUnix, granularity)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": usageResponse{
		Summary:    summary,
		ByAPIKey:   byAPIKey,
		ByModel:    byModel,
		ByProvider: byProvider,
		ByTime:     byTime,
	}})
}

func (h *UsageHandler) summary(ctx context.Context, from, to int64) (usageSummary, error) {
	var s usageSummary
	err := h.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(input_tokens + output_tokens + reasoning_tokens), 0),
			COALESCE(SUM(cost_usd), 0)
		FROM request_logs
		WHERE timestamp >= ? AND timestamp <= ?
	`, from, to).Scan(
		&s.Requests,
		&s.InputTokens,
		&s.OutputTokens,
		&s.ReasonTokens,
		&s.TotalTokens,
		&s.CostUSD,
	)
	return s, err
}

func (h *UsageHandler) byAPIKey(ctx context.Context, from, to int64) ([]usageBreakdown, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT
			COALESCE(rl.api_key_id, '__none__'),
			COALESCE(ak.name, ''),
			COUNT(*),
			COALESCE(SUM(rl.input_tokens), 0),
			COALESCE(SUM(rl.output_tokens), 0),
			COALESCE(SUM(rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.input_tokens + rl.output_tokens + rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.cost_usd), 0)
		FROM request_logs rl
		LEFT JOIN api_keys ak ON ak.id = rl.api_key_id
		WHERE rl.timestamp >= ? AND rl.timestamp <= ?
		GROUP BY rl.api_key_id
		ORDER BY total_tokens DESC
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]usageBreakdown, 0)
	for rows.Next() {
		var b usageBreakdown
		if err := rows.Scan(&b.APIKeyID, &b.APIKeyName, &b.Requests, &b.InputTokens, &b.OutputTokens, &b.ReasonTokens, &b.TotalTokens, &b.CostUSD); err != nil {
			return nil, err
		}
		if b.APIKeyID == "__none__" {
			b.APIKeyID = ""
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (h *UsageHandler) byModel(ctx context.Context, from, to int64) ([]usageBreakdown, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT
			COALESCE(model_id, '__unknown__'),
			COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(reasoning_tokens), 0),
			COALESCE(SUM(input_tokens + output_tokens + reasoning_tokens), 0),
			COALESCE(SUM(cost_usd), 0)
		FROM request_logs
		WHERE timestamp >= ? AND timestamp <= ?
		GROUP BY model_id
		ORDER BY total_tokens DESC
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]usageBreakdown, 0)
	for rows.Next() {
		var b usageBreakdown
		if err := rows.Scan(&b.ModelID, &b.Requests, &b.InputTokens, &b.OutputTokens, &b.ReasonTokens, &b.TotalTokens, &b.CostUSD); err != nil {
			return nil, err
		}
		if b.ModelID == "__unknown__" {
			b.ModelID = "unknown"
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (h *UsageHandler) byProvider(ctx context.Context, from, to int64) ([]usageBreakdown, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT
			COALESCE(rl.provider_type_id, '__unknown__'),
			COALESCE(pt.display_name, rl.provider_type_id),
			COUNT(*),
			COALESCE(SUM(rl.input_tokens), 0),
			COALESCE(SUM(rl.output_tokens), 0),
			COALESCE(SUM(rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.input_tokens + rl.output_tokens + rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.cost_usd), 0)
		FROM request_logs rl
		LEFT JOIN provider_types pt ON pt.id = rl.provider_type_id
		WHERE rl.timestamp >= ? AND rl.timestamp <= ?
		GROUP BY rl.provider_type_id
		ORDER BY total_tokens DESC
	`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]usageBreakdown, 0)
	for rows.Next() {
		var b usageBreakdown
		if err := rows.Scan(&b.ProviderID, &b.ProviderName, &b.Requests, &b.InputTokens, &b.OutputTokens, &b.ReasonTokens, &b.TotalTokens, &b.CostUSD); err != nil {
			return nil, err
		}
		if b.ProviderID == "__unknown__" {
			b.ProviderID = "unknown"
			b.ProviderName = "Unknown"
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (h *UsageHandler) byTime(ctx context.Context, from, to int64, granularity string) ([]timeBucket, error) {
	var bucketExpr string
	if granularity == "month" {
		bucketExpr = `strftime('%Y-%m', timestamp, 'unixepoch')`
	} else {
		bucketExpr = `strftime('%Y-%m-%d', timestamp, 'unixepoch')`
	}

	rows, err := h.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			%s AS bucket,
			COUNT(*),
			COALESCE(SUM(input_tokens + output_tokens + reasoning_tokens), 0),
			COALESCE(SUM(cost_usd), 0)
		FROM request_logs
		WHERE timestamp >= ? AND timestamp <= ?
		GROUP BY bucket
		ORDER BY bucket ASC
	`, bucketExpr), from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]timeBucket, 0)
	for rows.Next() {
		var b timeBucket
		if err := rows.Scan(&b.Bucket, &b.Requests, &b.Tokens, &b.CostUSD); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func parseDate(v string, fallback time.Time) time.Time {
	if v == "" {
		return fallback
	}
	t, err := time.Parse(time.DateOnly, v)
	if err != nil {
		return fallback
	}
	return t.UTC()
}
