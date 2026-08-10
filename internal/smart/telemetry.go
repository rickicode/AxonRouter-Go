package smart

import (
	"database/sql"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

const defaultTelemetryWindow = 60 * time.Minute
const defaultTelemetryTTL = 60 * time.Second

// TelemetryCache loads and caches per-model telemetry from request_logs.
type TelemetryCache struct {
	db         *sql.DB
	window     time.Duration
	ttl        time.Duration
	mu         sync.RWMutex
	cached     map[string]Telemetry
	cachedAt   time.Time
	clock      func() time.Time
}

// newTelemetryCache creates a telemetry cache with sensible defaults.
func newTelemetryCache(database *sql.DB) *TelemetryCache {
	return &TelemetryCache{
		db:     database,
		window: defaultTelemetryWindow,
		ttl:    defaultTelemetryTTL,
		cached: make(map[string]Telemetry),
		clock:  func() time.Time { return time.Now() },
	}
}

// Get returns telemetry for a model id. Missing telemetry is synthesised from
// model pricing so scoring stays deterministic even for brand-new candidates.
func (tc *TelemetryCache) Get(modelID string) Telemetry {
	m := tc.GetAll()
	if t, ok := m[modelID]; ok {
		return t
	}
	return telemetryFromPricing(modelID)
}

// GetAll returns the most recent per-model telemetry map.
func (tc *TelemetryCache) GetAll() map[string]Telemetry {
	tc.mu.RLock()
	if tc.cached != nil && tc.clock().Sub(tc.cachedAt) < tc.ttl {
		defer tc.mu.RUnlock()
		return tc.cached
	}
	tc.mu.RUnlock()

	tc.mu.Lock()
	defer tc.mu.Unlock()
	// Double-check after acquiring write lock.
	if tc.cached != nil && tc.clock().Sub(tc.cachedAt) < tc.ttl {
		return tc.cached
	}

	fresh := tc.load()
	tc.cached = fresh
	tc.cachedAt = tc.clock()
	return fresh
}

// Refresh invalidates the cache so the next Get reloads from the DB.
func (tc *TelemetryCache) Refresh() {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.cachedAt = time.Time{}
}

func (tc *TelemetryCache) load() map[string]Telemetry {
	out := make(map[string]Telemetry)
	if tc.db == nil {
		return out
	}

	since := tc.clock().Add(-tc.window).UnixMilli()
	rows, err := tc.db.Query(`
		SELECT
			model_id,
			COUNT(*) AS requests,
			AVG(latency_ms) AS avg_latency_ms,
			SUM(CASE WHEN status_code >= 400 OR COALESCE(error_message,'') != '' THEN 1 ELSE 0 END) AS errors,
			SUM(cost_usd) AS cost_usd,
			SUM(COALESCE(input_tokens,0) + COALESCE(output_tokens,0) + COALESCE(reasoning_tokens,0)) AS total_tokens
		FROM request_logs
		WHERE timestamp > ?
		GROUP BY model_id
	`, since)
	if err != nil {
		return out
	}
	defer rows.Close()

	for rows.Next() {
		var modelID string
		var requests, errors, totalTokens int64
		var avgLatencyMs, costUSD float64
		if err := rows.Scan(&modelID, &requests, &avgLatencyMs, &errors, &costUSD, &totalTokens); err != nil {
			continue
		}
		t := Telemetry{
			Requests:        requests,
			AvgLatencyMs:    avgLatencyMs,
			CostPer1KTokens: costPer1K(costUSD, totalTokens),
		}
		if requests > 0 {
			t.SuccessRate = float64(requests-errors) / float64(requests)
		} else {
			t.SuccessRate = 1.0
		}
		if t.SuccessRate < 0 {
			t.SuccessRate = 0
		}
		out[modelID] = t
	}
	return out
}

func costPer1K(costUSD float64, totalTokens int64) float64 {
	if totalTokens <= 0 {
		return 0
	}
	return (costUSD / float64(totalTokens)) * 1000.0
}

func telemetryFromPricing(modelID string) Telemetry {
	p := usage.GetPricing(modelID)
	avgCost := (p.InputPer1K + p.OutputPer1K) / 2.0
	if avgCost <= 0 {
		avgCost = 0.001
	}
	return Telemetry{
		Requests:        0,
		AvgLatencyMs:    1000.0,
		SuccessRate:     0.995,
		CostPer1KTokens: avgCost,
	}
}
