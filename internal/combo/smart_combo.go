package combo

import (
	"database/sql"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/db"
)

// SmartGoal defines the combo selection strategy.
type SmartGoal string

const (
	GoalAuto     SmartGoal = "auto"
	GoalEconomy  SmartGoal = "economy"
	GoalBalanced SmartGoal = "balanced"
	GoalPremium  SmartGoal = "premium"
)

// Telemetry holds recent performance data for smart combo decisions.
type Telemetry struct {
	ErrorRate    float64
	FallbackRate float64
	TotalCost    float64
	AvgLatency   float64
	WindowMin    int
}

// SmartCombo resolves which combo to use based on a goal.
// Telemetry is cached in memory with a short TTL so the smart path does not
// query SQLite on every request.
type SmartCombo struct {
	mu              sync.RWMutex
	db              *sql.DB
	cachedTelemetry *Telemetry
	cachedAt        time.Time
	cacheTTL        time.Duration
}

// NewSmartCombo creates a new smart combo resolver.
func NewSmartCombo(database *sql.DB) *SmartCombo {
	return &SmartCombo{
		db:       database,
		cacheTTL: 60 * time.Second,
	}
}

// Resolve selects the best combo for the given goal from the provided smart combos.
// Telemetry is resolved internally (and cached).
func (sc *SmartCombo) Resolve(goal SmartGoal, combos []*db.Combo) (*db.Combo, error) {
	if len(combos) == 0 {
		return nil, nil
	}

	telemetry := sc.GetTelemetry(60)

	switch goal {
	case GoalAuto:
		return sc.resolveAuto(combos, telemetry), nil
	case GoalEconomy:
		return sc.findByGoal(combos, "economy"), nil
	case GoalPremium:
		return sc.findByGoal(combos, "premium"), nil
	default: // balanced
		return sc.findByGoal(combos, "balanced"), nil
	}
}

// resolveAuto dynamically picks a goal based on telemetry.
// Thresholds use rate-normalized values so they stay meaningful regardless of
// the telemetry window.
func (sc *SmartCombo) resolveAuto(combos []*db.Combo, telemetry *Telemetry) *db.Combo {
	if telemetry == nil || telemetry.WindowMin <= 0 {
		return sc.findByGoal(combos, "balanced")
	}

	costPerMinute := telemetry.TotalCost / float64(telemetry.WindowMin)

	// High error rate → escalate to premium
	if telemetry.ErrorRate >= 0.15 || telemetry.FallbackRate >= 0.2 {
		if c := sc.findByGoal(combos, "premium"); c != nil {
			return c
		}
	}
	// High burn rate → shift to economy
	if costPerMinute >= 0.85 {
		if c := sc.findByGoal(combos, "economy"); c != nil {
			return c
		}
	}
	return sc.findByGoal(combos, "balanced")
}

// findByGoal returns the first smart combo matching the goal.
func (sc *SmartCombo) findByGoal(combos []*db.Combo, goal string) *db.Combo {
	for _, c := range combos {
		if c.SmartGoal.Valid && c.SmartGoal.String == goal {
			return c
		}
	}
	return nil
}

// GetTelemetry computes recent telemetry from request logs with in-memory caching.
func (sc *SmartCombo) GetTelemetry(minutes int) *Telemetry {
	if minutes <= 0 {
		minutes = 60
	}

	sc.mu.RLock()
	if sc.cachedTelemetry != nil && time.Since(sc.cachedAt) < sc.cacheTTL {
		sc.mu.RUnlock()
		return sc.cachedTelemetry
	}
	sc.mu.RUnlock()

	var total, errors int64
	var cost float64
	var latencySum int64

	since := timeNow().Add(-timeMinutes(minutes)).UnixMilli()
	sc.db.QueryRow(`
		SELECT COUNT(*), 
		       SUM(CASE WHEN error_message IS NOT NULL AND error_message != '' THEN 1 ELSE 0 END),
		       SUM(cost_usd),
		       COALESCE(SUM(latency_ms), 0)
		FROM request_logs WHERE timestamp > ?
	`, since).Scan(&total, &errors, &cost, &latencySum)

	tel := &Telemetry{WindowMin: minutes}
	if total > 0 {
		tel.ErrorRate = float64(errors) / float64(total)
		tel.TotalCost = cost
		tel.AvgLatency = float64(latencySum) / float64(total)
	}

	sc.mu.Lock()
	sc.cachedTelemetry = tel
	sc.cachedAt = timeNow()
	sc.mu.Unlock()

	return tel
}

// timeNow and timeMinutes are functions so tests can mock them.
var timeNow = func() time.Time { return time.Now() }
var timeMinutes = func(m int) time.Duration { return time.Duration(m) * time.Minute }
