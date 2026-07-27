package smart

import (
	"database/sql"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

// ModelTelemetry aggregates recent request log data for one concrete model.
type ModelTelemetry struct {
	ModelID        string  `json:"model_id"`
	AvgLatencyMs   float64 `json:"avg_latency_ms"`
	SuccessRate    float64 `json:"success_rate"`
	CostPer1kTok  float64 `json:"cost_per_1k_tokens"`
	TotalReqs      int64   `json:"total_reqs"`
	ErrorCount     int64   `json:"error_count"`
	TotalTokens    int64   `json:"total_tokens"`
}

// TelemetryStore computes and caches per-model telemetry from request_logs.
type TelemetryStore struct {
	mu       sync.RWMutex
	db       *sql.DB
	cache    map[string]*ModelTelemetry
	cachedAt time.Time
	ttl      time.Duration
	window   time.Duration
}

// NewTelemetryStore creates a telemetry store with a 60s refresh default.
func NewTelemetryStore(database *sql.DB) *TelemetryStore {
	return &TelemetryStore{
		db:     database,
		cache:  make(map[string]*ModelTelemetry),
		ttl:    60 * time.Second,
		window: 15 * time.Minute,
	}
}

// Get returns telemetry for modelID, using cached data when fresh.
func (ts *TelemetryStore) Get(modelID string) *ModelTelemetry {
	ts.mu.RLock()
	if ts.ttl == 0 || time.Since(ts.cachedAt) < ts.ttl {
		if t, ok := ts.cache[modelID]; ok {
			ts.mu.RUnlock()
			return t
		}
	}
	ts.mu.RUnlock()
	ts.refresh()
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.cache[modelID]
}

// All returns the latest telemetry for all models.
func (ts *TelemetryStore) All() map[string]*ModelTelemetry {
	ts.refresh()
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	out := make(map[string]*ModelTelemetry, len(ts.cache))
	for k, v := range ts.cache {
		out[k] = v
	}
	return out
}

func (ts *TelemetryStore) refresh() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.db == nil {
		ts.cachedAt = time.Now()
		return
	}
	if ts.ttl > 0 && time.Since(ts.cachedAt) < ts.ttl {
		return
	}
	since := time.Now().Add(-ts.window).UnixMilli()
	rows, err := ts.db.Query(`
		SELECT model_id,
			AVG(latency_ms),
			SUM(CASE WHEN COALESCE(error_message,'') != '' OR COALESCE(status_code,0) >= 500 THEN 1 ELSE 0 END) as errors,
			COUNT(*),
			SUM(input_tokens+output_tokens+reasoning_tokens),
			SUM(cost_usd)
		FROM request_logs
		WHERE timestamp > ? AND model_id IS NOT NULL AND model_id != ''
		GROUP BY model_id
	`, since)
	if err != nil {
		logging.Logger.Warn("smart telemetry refresh failed", "error", err)
		ts.cachedAt = time.Now()
		return
	}
	defer rows.Close()
	cache := make(map[string]*ModelTelemetry)
	for rows.Next() {
		var modelID sql.NullString
		var avgLatency sql.NullFloat64
		var errors, total, tokens sql.NullInt64
		var cost sql.NullFloat64
		if err := rows.Scan(&modelID, &avgLatency, &errors, &total, &tokens, &cost); err != nil {
			logging.Logger.Warn("smart telemetry scan failed", "error", err)
			continue
		}
		if !modelID.Valid || modelID.String == "" {
			continue
		}
		tel := &ModelTelemetry{ModelID: modelID.String}
		if total.Valid && total.Int64 > 0 {
			tel.TotalReqs = total.Int64
			tel.SuccessRate = 1.0 - float64(errors.Int64)/float64(total.Int64)
		}
		if avgLatency.Valid {
			tel.AvgLatencyMs = avgLatency.Float64
		}
		if tokens.Valid && tokens.Int64 > 0 {
			tel.TotalTokens = tokens.Int64
			tel.CostPer1kTok = (cost.Float64 / float64(tokens.Int64)) * 1000.0
		}
		cache[modelID.String] = tel
	}
	if err := rows.Err(); err != nil {
		logging.Logger.Warn("smart telemetry rows error", "error", err)
	}
	ts.cache = cache
	ts.cachedAt = time.Now()
}

// SetWindow sets the lookback window used when aggregating telemetry.
func (ts *TelemetryStore) SetWindow(d time.Duration) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.window = d
	ts.cachedAt = time.Time{}
}

// SetTTL sets the in-memory cache refresh interval.
func (ts *TelemetryStore) SetTTL(d time.Duration) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.ttl = d
	ts.cachedAt = time.Time{}
}
