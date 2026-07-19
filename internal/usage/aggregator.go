package usage

import (
	"database/sql"
	"time"
)

// Aggregator computes usage summaries from request_logs.
type Aggregator struct {
	db *sql.DB
}

// NewAggregator creates a new usage aggregator.
func NewAggregator(database *sql.DB) *Aggregator {
	return &Aggregator{db: database}
}

// ProviderUsage holds aggregated usage for a provider.
type ProviderUsage struct {
	ProviderTypeID string  `json:"provider_type_id"`
	Requests       int64   `json:"requests"`
	InputTokens    int64   `json:"input_tokens"`
	OutputTokens   int64   `json:"output_tokens"`
	CachedTokens   int64   `json:"cached_tokens"`
	TotalTokens    int64   `json:"total_tokens"`
	CostUsd        float64 `json:"cost_usd"`
	Errors         int64   `json:"errors"`
	AvgLatencyMs   float64 `json:"avg_latency_ms"`
}

// ModelUsage holds aggregated usage for a model.
type ModelUsage struct {
	ModelID      string  `json:"model_id"`
	Requests     int64   `json:"requests"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CachedTokens int64   `json:"cached_tokens"`
	CostUsd      float64 `json:"cost_usd"`
	Errors       int64   `json:"errors"`
}

// DailyUsage holds a single day's aggregated stats.
type DailyUsage struct {
	Date       string  `json:"date"`
	Requests   int64   `json:"requests"`
	Tokens     int64   `json:"tokens"`
	CostUsd    float64 `json:"cost_usd"`
	Errors     int64   `json:"errors"`
}

// GetProviderUsage returns per-provider usage for the last N hours.
func (a *Aggregator) GetProviderUsage(hours int) ([]ProviderUsage, error) {
	if hours <= 0 {
		hours = 24
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour).UnixMilli()

	rows, err := a.db.Query(`
		SELECT COALESCE(provider_type_id, 'unknown'),
		       COUNT(*),
		       COALESCE(SUM(input_tokens), 0),
		       COALESCE(SUM(output_tokens), 0),
		       COALESCE(SUM(cached_tokens), 0),
		       COALESCE(SUM(input_tokens + output_tokens), 0),
		       COALESCE(SUM(cost_usd), 0),
		       SUM(CASE WHEN error_message IS NOT NULL AND error_message != '' THEN 1 ELSE 0 END),
		       COALESCE(AVG(latency_ms), 0)
		FROM request_logs WHERE timestamp > ?
		GROUP BY provider_type_id
		ORDER BY COUNT(*) DESC
	`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ProviderUsage
	for rows.Next() {
		var u ProviderUsage
		rows.Scan(&u.ProviderTypeID, &u.Requests, &u.InputTokens,
			&u.OutputTokens, &u.CachedTokens, &u.TotalTokens, &u.CostUsd,
			&u.Errors, &u.AvgLatencyMs)
		result = append(result, u)
	}
	return result, nil
}

// GetModelUsage returns per-model usage for the last N hours.
func (a *Aggregator) GetModelUsage(hours int) ([]ModelUsage, error) {
	if hours <= 0 {
		hours = 24
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour).UnixMilli()

	rows, err := a.db.Query(`
		SELECT COALESCE(model_id, 'unknown'),
		       COUNT(*),
		       COALESCE(SUM(input_tokens), 0),
		       COALESCE(SUM(output_tokens), 0),
		       COALESCE(SUM(cached_tokens), 0),
		       COALESCE(SUM(cost_usd), 0),
		       SUM(CASE WHEN error_message IS NOT NULL AND error_message != '' THEN 1 ELSE 0 END)
		FROM request_logs WHERE timestamp > ?
		GROUP BY model_id
		ORDER BY COUNT(*) DESC
		LIMIT 20
	`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ModelUsage
	for rows.Next() {
		var u ModelUsage
		rows.Scan(&u.ModelID, &u.Requests, &u.InputTokens,
			&u.OutputTokens, &u.CachedTokens, &u.CostUsd, &u.Errors)
		result = append(result, u)
	}
	return result, nil
}

// GetDailyUsage returns daily usage for the last N days.
func (a *Aggregator) GetDailyUsage(days int) ([]DailyUsage, error) {
	if days <= 0 {
		days = 30
	}
	since := time.Now().AddDate(0, 0, -days).UnixMilli()

	rows, err := a.db.Query(`
		SELECT date(timestamp / 1000, 'unixepoch') as day,
		       COUNT(*),
		       COALESCE(SUM(input_tokens + output_tokens), 0),
		       COALESCE(SUM(cost_usd), 0),
		       SUM(CASE WHEN error_message IS NOT NULL AND error_message != '' THEN 1 ELSE 0 END)
		FROM request_logs WHERE timestamp > ?
		GROUP BY day
		ORDER BY day DESC
	`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []DailyUsage
	for rows.Next() {
		var u DailyUsage
		rows.Scan(&u.Date, &u.Requests, &u.Tokens, &u.CostUsd, &u.Errors)
		result = append(result, u)
	}
	return result, nil
}

// TodaySummary holds aggregate usage stats for a single day window.
type TodaySummary struct {
	Requests     int64   `json:"requests"`
	Tokens       int64   `json:"tokens"`
	CostUsd      float64 `json:"cost_usd"`
	Errors       int64   `json:"errors"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
}

// GetTodaySummary returns today's aggregate stats including errors and latency.
func (a *Aggregator) GetTodaySummary() (TodaySummary, error) {
	var s TodaySummary
	today := time.Now().Truncate(24 * time.Hour).UnixMilli()
	err := a.db.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(input_tokens + output_tokens), 0),
		       COALESCE(SUM(cost_usd), 0),
		       COALESCE(SUM(CASE WHEN error_message IS NOT NULL AND error_message != '' THEN 1 ELSE 0 END), 0),
		       COALESCE(AVG(latency_ms), 0)
		FROM request_logs WHERE timestamp >= ?
	`, today).Scan(&s.Requests, &s.Tokens, &s.CostUsd, &s.Errors, &s.AvgLatencyMs)
	return s, err
}

// GetDaySummary returns aggregate stats for the day offset from today
// (0 = today, -1 = yesterday).
func (a *Aggregator) GetDaySummary(dayOffset int) (TodaySummary, error) {
	var s TodaySummary
	day := time.Now().AddDate(0, 0, dayOffset).Truncate(24 * time.Hour)
	start := day.UnixMilli()
	end := day.Add(24 * time.Hour).UnixMilli()
	err := a.db.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(input_tokens + output_tokens), 0),
		       COALESCE(SUM(cost_usd), 0),
		       COALESCE(SUM(CASE WHEN error_message IS NOT NULL AND error_message != '' THEN 1 ELSE 0 END), 0),
		       COALESCE(AVG(latency_ms), 0)
		FROM request_logs WHERE timestamp >= ? AND timestamp < ?
	`, start, end).Scan(&s.Requests, &s.Tokens, &s.CostUsd, &s.Errors, &s.AvgLatencyMs)
	return s, err
}

// GetMonthToDateSummary returns aggregate stats from the start of the current UTC month until now.
func (a *Aggregator) GetMonthToDateSummary() (TodaySummary, error) {
	var s TodaySummary
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	end := now.UnixMilli()
	err := a.db.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(input_tokens + output_tokens), 0),
		       COALESCE(SUM(cost_usd), 0),
		       COALESCE(SUM(CASE WHEN error_message IS NOT NULL AND error_message != '' THEN 1 ELSE 0 END), 0),
		       COALESCE(AVG(latency_ms), 0)
		FROM request_logs WHERE timestamp >= ? AND timestamp <= ?
	`, start, end).Scan(&s.Requests, &s.Tokens, &s.CostUsd, &s.Errors, &s.AvgLatencyMs)
	return s, err
}

// GetTodayStats returns today's aggregate stats.
func (a *Aggregator) GetTodayStats() (requests int64, tokens int64, cost float64, err error) {
	today := time.Now().Truncate(24 * time.Hour).UnixMilli()
	err = a.db.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(input_tokens + output_tokens), 0), COALESCE(SUM(cost_usd), 0)
		FROM request_logs WHERE timestamp >= ?
	`, today).Scan(&requests, &tokens, &cost)
	return
}
