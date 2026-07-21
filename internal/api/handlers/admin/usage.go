package admin

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// UsageHandler aggregates request_logs into rich usage reports.
type UsageHandler struct {
	db *sql.DB
}

// NewUsageHandler creates a new usage handler.
func NewUsageHandler(db *sql.DB) *UsageHandler {
	return &UsageHandler{db: db}
}

// usageFilters holds all optional dimensions the report can be sliced by.
type usageFilters struct {
	From        int64
	To          int64
	Granularity string
	APIKeyID    string
	ModelID     string
	ProviderID  string
	Modality    string
	StatusCode  int
}

type usageBreakdown struct {
	APIKeyID     string  `json:"api_key_id,omitempty"`
	APIKeyName   string  `json:"api_key_name,omitempty"`
	ModelID      string  `json:"model_id,omitempty"`
	ProviderID   string  `json:"provider_id,omitempty"`
	ProviderName string  `json:"provider_name,omitempty"`
	StatusCode   int     `json:"status_code,omitempty"`
	Modality     string  `json:"modality,omitempty"`
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	ReasonTokens int64   `json:"reasoning_tokens"`
	TotalTokens  int64   `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	Errors       int64   `json:"errors"`
	ErrorRate    float64 `json:"error_rate"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	FirstAt      *int64  `json:"first_request_at,omitempty"`
	LastAt       *int64  `json:"last_request_at,omitempty"`
}

type timeBucket struct {
	Bucket       string  `json:"bucket"`
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	ReasonTokens int64   `json:"reasoning_tokens"`
	Tokens       int64   `json:"tokens"`
	CostUSD      float64 `json:"cost_usd"`
}

type activityDay struct {
	Date     string  `json:"date"`
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	CostUSD  float64 `json:"cost_usd"`
}

type activityResponse struct {
	From string        `json:"from"`
	To   string        `json:"to"`
	Days []activityDay `json:"days"`
}

type usageSummary struct {
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	ReasonTokens int64   `json:"reasoning_tokens"`
	TotalTokens  int64   `json:"total_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	Errors       int64   `json:"errors"`
	ErrorRate    float64 `json:"error_rate"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
}

type usageResponse struct {
	Summary    usageSummary     `json:"summary"`
	ByAPIKey   []usageBreakdown `json:"by_api_key"`
	ByModel    []usageBreakdown `json:"by_model"`
	ByProvider []usageBreakdown `json:"by_provider"`
	ByModality []usageBreakdown `json:"by_modality"`
	ByStatus   []usageBreakdown `json:"by_status"`
	ByTime     []timeBucket     `json:"by_time"`
	Filters    usageFilters     `json:"filters"`
}

// Get aggregates usage across request_logs with optional filtering.
// Query params:
//   - from/to: ISO dates (YYYY-MM-DD), default to last 30 days.
//   - granularity: "day" or "month" (default "day").
//   - api_key_id, model_id, provider_id, modality: exact filters.
//   - status_code: exact HTTP status filter.
func (h *UsageHandler) Get(c *gin.Context) {
	f := parseFilters(c)

	ctx := c.Request.Context()
	summary, err := h.summary(ctx, f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	byAPIKey, err := h.byAPIKey(ctx, f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	byModel, err := h.byModel(ctx, f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	byProvider, err := h.byProvider(ctx, f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	byModality, err := h.byModality(ctx, f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	byStatus, err := h.byStatus(ctx, f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	byTime, err := h.byTime(ctx, f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": usageResponse{
		Summary:    summary,
		ByAPIKey:   byAPIKey,
		ByModel:    byModel,
		ByProvider: byProvider,
		ByModality: byModality,
		ByStatus:   byStatus,
		ByTime:     byTime,
		Filters:    f,
	}})
}

func baseWhere(f usageFilters) (string, []interface{}) {
	var clauses []string
	args := []interface{}{f.From, f.To}
	clauses = append(clauses, "rl.timestamp >= ? AND rl.timestamp <= ?")
	if f.APIKeyID != "" {
		clauses = append(clauses, "rl.api_key_id = ?")
		args = append(args, f.APIKeyID)
	}
	if f.ModelID != "" {
		clauses = append(clauses, "rl.model_id = ?")
		args = append(args, f.ModelID)
	}
	if f.ProviderID != "" {
		clauses = append(clauses, "rl.provider_type_id = ?")
		args = append(args, f.ProviderID)
	}
	if f.Modality != "" {
		clauses = append(clauses, "rl.modality = ?")
		args = append(args, f.Modality)
	}
	if f.StatusCode > 0 {
		clauses = append(clauses, "rl.status_code = ?")
		args = append(args, f.StatusCode)
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

func (h *UsageHandler) summary(ctx context.Context, f usageFilters) (usageSummary, error) {
	var s usageSummary
	where, args := baseWhere(f)
	err := h.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT
			COUNT(*),
			COALESCE(SUM(rl.input_tokens), 0),
			COALESCE(SUM(rl.output_tokens), 0),
			COALESCE(SUM(rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.input_tokens + rl.output_tokens + rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.cost_usd), 0),
			COALESCE(SUM(CASE WHEN rl.status_code >= 400 OR rl.error_message IS NOT NULL THEN 1 ELSE 0 END), 0),
			COALESCE(AVG(rl.latency_ms), 0)
		FROM request_logs rl
		%s
	`, where), args...).Scan(
		&s.Requests,
		&s.InputTokens,
		&s.OutputTokens,
		&s.ReasonTokens,
		&s.TotalTokens,
		&s.CostUSD,
		&s.Errors,
		&s.AvgLatencyMs,
	)
	if s.Requests > 0 {
		s.ErrorRate = float64(s.Errors) / float64(s.Requests)
	}
	return s, err
}

func (h *UsageHandler) byAPIKey(ctx context.Context, f usageFilters) ([]usageBreakdown, error) {
	where, args := baseWhere(f)
	rows, err := h.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			COALESCE(rl.api_key_id, '__none__'),
			COALESCE(ak.name, ''),
			COUNT(*),
			COALESCE(SUM(rl.input_tokens), 0),
			COALESCE(SUM(rl.output_tokens), 0),
			COALESCE(SUM(rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.input_tokens + rl.output_tokens + rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.cost_usd), 0),
			COALESCE(SUM(CASE WHEN rl.status_code >= 400 OR rl.error_message IS NOT NULL THEN 1 ELSE 0 END), 0),
			COALESCE(AVG(rl.latency_ms), 0),
			MIN(rl.timestamp),
			MAX(rl.timestamp)
		FROM request_logs rl
		LEFT JOIN api_keys ak ON ak.id = rl.api_key_id
		%s
		GROUP BY rl.api_key_id
		ORDER BY 6 DESC
	`, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBreakdowns(rows, true, false, false, false)
}

func (h *UsageHandler) byModel(ctx context.Context, f usageFilters) ([]usageBreakdown, error) {
	where, args := baseWhere(f)
	rows, err := h.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			COALESCE(rl.model_id, '__unknown__'),
			COUNT(*),
			COALESCE(SUM(rl.input_tokens), 0),
			COALESCE(SUM(rl.output_tokens), 0),
			COALESCE(SUM(rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.input_tokens + rl.output_tokens + rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.cost_usd), 0),
			COALESCE(SUM(CASE WHEN rl.status_code >= 400 OR rl.error_message IS NOT NULL THEN 1 ELSE 0 END), 0),
			COALESCE(AVG(rl.latency_ms), 0),
			MIN(rl.timestamp),
			MAX(rl.timestamp)
		FROM request_logs rl
		%s
		GROUP BY rl.model_id
		ORDER BY 6 DESC
	`, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBreakdowns(rows, false, true, false, false)
}

func (h *UsageHandler) byProvider(ctx context.Context, f usageFilters) ([]usageBreakdown, error) {
	where, args := baseWhere(f)
	rows, err := h.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			COALESCE(rl.provider_type_id, '__unknown__'),
			COALESCE(pt.display_name, rl.provider_type_id),
			COUNT(*),
			COALESCE(SUM(rl.input_tokens), 0),
			COALESCE(SUM(rl.output_tokens), 0),
			COALESCE(SUM(rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.input_tokens + rl.output_tokens + rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.cost_usd), 0),
			COALESCE(SUM(CASE WHEN rl.status_code >= 400 OR rl.error_message IS NOT NULL THEN 1 ELSE 0 END), 0),
			COALESCE(AVG(rl.latency_ms), 0),
			MIN(rl.timestamp),
			MAX(rl.timestamp)
		FROM request_logs rl
		LEFT JOIN provider_types pt ON pt.id = rl.provider_type_id
		%s
		GROUP BY rl.provider_type_id
		ORDER BY 6 DESC
	`, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBreakdowns(rows, false, false, true, false)
}

func (h *UsageHandler) byModality(ctx context.Context, f usageFilters) ([]usageBreakdown, error) {
	where, args := baseWhere(f)
	rows, err := h.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			COALESCE(rl.modality, '__unknown__'),
			COUNT(*),
			COALESCE(SUM(rl.input_tokens), 0),
			COALESCE(SUM(rl.output_tokens), 0),
			COALESCE(SUM(rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.input_tokens + rl.output_tokens + rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.cost_usd), 0),
			COALESCE(SUM(CASE WHEN rl.status_code >= 400 OR rl.error_message IS NOT NULL THEN 1 ELSE 0 END), 0),
			COALESCE(AVG(rl.latency_ms), 0),
			MIN(rl.timestamp),
			MAX(rl.timestamp)
		FROM request_logs rl
		%s
		GROUP BY rl.modality
		ORDER BY 6 DESC
	`, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBreakdowns(rows, false, false, false, true)
}

func (h *UsageHandler) byStatus(ctx context.Context, f usageFilters) ([]usageBreakdown, error) {
	where, args := baseWhere(f)
	rows, err := h.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			COALESCE(rl.status_code, 0),
			COUNT(*),
			COALESCE(SUM(rl.input_tokens), 0),
			COALESCE(SUM(rl.output_tokens), 0),
			COALESCE(SUM(rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.input_tokens + rl.output_tokens + rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.cost_usd), 0),
			COALESCE(SUM(CASE WHEN rl.status_code >= 400 OR rl.error_message IS NOT NULL THEN 1 ELSE 0 END), 0),
			COALESCE(AVG(rl.latency_ms), 0),
			MIN(rl.timestamp),
			MAX(rl.timestamp)
		FROM request_logs rl
		%s
		GROUP BY rl.status_code
		ORDER BY 2 DESC
	`, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]usageBreakdown, 0)
	for rows.Next() {
		var b usageBreakdown
		var first, last sql.NullInt64
		if err := rows.Scan(&b.StatusCode, &b.Requests, &b.InputTokens, &b.OutputTokens, &b.ReasonTokens, &b.TotalTokens, &b.CostUSD, &b.Errors, &b.AvgLatencyMs, &first, &last); err != nil {
			return nil, err
		}
		if b.Requests > 0 {
			b.ErrorRate = float64(b.Errors) / float64(b.Requests)
		}
		if first.Valid {
			v := first.Int64
			b.FirstAt = &v
		}
		if last.Valid {
			v := last.Int64
			b.LastAt = &v
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (h *UsageHandler) byTime(ctx context.Context, f usageFilters) ([]timeBucket, error) {
	where, args := baseWhere(f)
	// rl.timestamp is stored in MILLISECONDS; strftime's 'unixepoch' modifier
	// expects SECONDS, so divide by 1000 (same fix as aggregator.GetDailyUsage).
	bucketExpr := `strftime('%Y-%m-%d', rl.timestamp / 1000, 'unixepoch')`
	if f.Granularity == "month" {
		bucketExpr = `strftime('%Y-%m', rl.timestamp / 1000, 'unixepoch')`
	}
	rows, err := h.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			%s AS bucket,
			COUNT(*),
			COALESCE(SUM(rl.input_tokens), 0),
			COALESCE(SUM(rl.output_tokens), 0),
			COALESCE(SUM(rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.input_tokens + rl.output_tokens + rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.cost_usd), 0)
		FROM request_logs rl
		%s
		GROUP BY bucket
		ORDER BY bucket ASC
	`, bucketExpr, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]timeBucket, 0)
	for rows.Next() {
		var b timeBucket
		if err := rows.Scan(&b.Bucket, &b.Requests, &b.InputTokens, &b.OutputTokens, &b.ReasonTokens, &b.Tokens, &b.CostUSD); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// scanBreakdowns scans rows produced by the standard breakdown query shape.
// The shape must be: (label, [extra label], count, input, output, reason, total, cost, errors, avg_latency, min_ts, max_ts)
func scanBreakdowns(rows *sql.Rows, hasAPIKey bool, hasModel bool, hasProvider bool, hasModality bool) ([]usageBreakdown, error) {
	out := make([]usageBreakdown, 0)
	for rows.Next() {
		var b usageBreakdown
		var first, last sql.NullInt64
		var id string
		var name string
		switch {
		case hasAPIKey:
			if err := rows.Scan(&id, &name, &b.Requests, &b.InputTokens, &b.OutputTokens, &b.ReasonTokens, &b.TotalTokens, &b.CostUSD, &b.Errors, &b.AvgLatencyMs, &first, &last); err != nil {
				return nil, err
			}
			if id == "__none__" {
				id = "unauthenticated"
				name = "Unauthenticated"
			}
			b.APIKeyID = id
			b.APIKeyName = name
		case hasProvider:
			if err := rows.Scan(&id, &name, &b.Requests, &b.InputTokens, &b.OutputTokens, &b.ReasonTokens, &b.TotalTokens, &b.CostUSD, &b.Errors, &b.AvgLatencyMs, &first, &last); err != nil {
				return nil, err
			}
			if id == "__unknown__" {
				id = "unknown"
				name = "Unknown"
			}
			b.ProviderID = id
			b.ProviderName = name
		case hasModel, hasModality:
			if err := rows.Scan(&id, &b.Requests, &b.InputTokens, &b.OutputTokens, &b.ReasonTokens, &b.TotalTokens, &b.CostUSD, &b.Errors, &b.AvgLatencyMs, &first, &last); err != nil {
				return nil, err
			}
			if id == "__unknown__" {
				id = "unknown"
			}
			if hasModel {
				b.ModelID = id
			} else {
				b.Modality = id
			}
		}
		if b.Requests > 0 {
			b.ErrorRate = float64(b.Errors) / float64(b.Requests)
		}
		if first.Valid {
			v := first.Int64
			b.FirstAt = &v
		}
		if last.Valid {
			v := last.Int64
			b.LastAt = &v
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func parseFilters(c *gin.Context) usageFilters {
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

	statusCode := 0
	if v := c.Query("status_code"); v != "" {
		if n, err := parseInt(v); err == nil {
			statusCode = n
		}
	}

	return usageFilters{
		From:        from.UnixMilli(),
		To:          to.UnixMilli(),
		Granularity: granularity,
		APIKeyID:    c.Query("api_key_id"),
		ModelID:     c.Query("model_id"),
		ProviderID:  c.Query("provider_id"),
		Modality:    c.Query("modality"),
		StatusCode:  statusCode,
	}
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

func parseInt(v string) (int, error) {
	var n int
	_, err := fmt.Sscanf(v, "%d", &n)
	return n, err
}

// Summary returns compact usage stats for the dashboard system-metrics cards.
// GET /api/admin/usage/summary
func (h *UsageHandler) Summary(c *gin.Context) {
	agg := usage.NewAggregator(h.db)

	today, _ := agg.GetTodaySummary()
	yesterday, _ := agg.GetDaySummary(-1)
	month, _ := agg.GetMonthToDateSummary()

	now := time.Now().UTC()
	daysInMonth := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	daysElapsed := now.Day()
	if daysElapsed < 1 {
		daysElapsed = 1
	}
	projected := 0.0
	if month.CostUsd > 0 {
		projected = month.CostUsd / float64(daysElapsed) * float64(daysInMonth)
	}

	resets, _ := quota.NextProviderResets(h.db)
	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"today":               today,
			"yesterday":           yesterday,
			"month_to_date":       month,
			"projected_month_cost": projected,
			"next_quota_reset":    earliestReset(resets),
		},
	})
}

// Activity returns sparse per-day activity for the last 12 months in UTC,
// with the same dimension filters as /usage. Explicit from/to query params
// are ignored; the date range is always now-364 days through now inclusive.
func (h *UsageHandler) Activity(c *gin.Context) {
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -364).Truncate(24 * time.Hour)
	to := now.Truncate(24 * time.Hour).Add(24*time.Hour - time.Second)

	f := usageFilters{
		From:       from.UnixMilli(),
		To:         to.UnixMilli(),
		APIKeyID:   c.Query("api_key_id"),
		ModelID:    c.Query("model_id"),
		ProviderID: c.Query("provider_id"),
		Modality:   c.Query("modality"),
	}
	if v := c.Query("status_code"); v != "" {
		if n, err := parseInt(v); err == nil {
			f.StatusCode = n
		}
	}

	days, err := h.activity(c.Request.Context(), f)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": activityResponse{
		From: from.Format(time.DateOnly),
		To:   to.Format(time.DateOnly),
		Days: days,
	}})
}

func (h *UsageHandler) activity(ctx context.Context, f usageFilters) ([]activityDay, error) {
	where, args := baseWhere(f)
	rows, err := h.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			date(rl.timestamp / 1000, 'unixepoch') AS day,
			COUNT(*),
			COALESCE(SUM(rl.input_tokens + rl.output_tokens + rl.reasoning_tokens), 0),
			COALESCE(SUM(rl.cost_usd), 0)
		FROM request_logs rl
		%s
		GROUP BY day
		ORDER BY day ASC
	`, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]activityDay, 0)
	for rows.Next() {
		var d activityDay
		if err := rows.Scan(&d.Date, &d.Requests, &d.Tokens, &d.CostUSD); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
