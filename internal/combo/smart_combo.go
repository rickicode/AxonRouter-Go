package combo

import (
	"database/sql"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
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
	ErrorRate   float64
	TotalCost   float64
	AvgLatency  float64
	WindowMin   int
	TotalReqs   int64
	ErrorCount  int64
}

// Config holds configurable thresholds for smart combo decisions.
type Config struct {
	ErrorRateThreshold float64 // Error rate threshold to escalate to premium (default: 0.15)
	CostPerMinThreshold float64 // Cost per minute threshold to shift to economy (default: 0.85)
	TelemetryWindowMin  int     // Telemetry window in minutes (default: 60)
	CacheTTLSec         int     // Telemetry cache TTL in seconds (default: 60)
}

// SmartCombo resolves which combo to use based on a goal.
// Telemetry is cached in memory with a short TTL so the smart path does not
// query SQLite on every request.
type SmartCombo struct {
	mu             sync.RWMutex
	db             *sql.DB
	cachedTelemetry *Telemetry
	cachedAt       time.Time
	config         Config
}

// defaultConfig returns the default configuration with env var overrides.
func defaultConfig() Config {
	cfg := Config{
		ErrorRateThreshold:  0.15,
		CostPerMinThreshold: 0.85,
		TelemetryWindowMin:  60,
		CacheTTLSec:         60,
	}

	if v := os.Getenv("SMART_ERROR_RATE_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f <= 1 {
			cfg.ErrorRateThreshold = f
		}
	}
	if v := os.Getenv("SMART_COST_PER_MIN_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			cfg.CostPerMinThreshold = f
		}
	}
	if v := os.Getenv("SMART_TELEMETRY_WINDOW_MIN"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			cfg.TelemetryWindowMin = i
		}
	}
	if v := os.Getenv("SMART_CACHE_TTL_SEC"); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			cfg.CacheTTLSec = i
		}
	}

	return cfg
}

// NewSmartCombo creates a new smart combo resolver.
func NewSmartCombo(database *sql.DB) *SmartCombo {
	cfg := defaultConfig()
	return &SmartCombo{
		db:     database,
		config: cfg,
	}
}

// Resolve selects the best combo for the given goal from the provided smart combos.
// Telemetry is resolved internally (and cached).
func (sc *SmartCombo) Resolve(goal SmartGoal, combos []*db.Combo) (*db.Combo, error) {
	if len(combos) == 0 {
		logging.Logger.Warn("smart combo: no smart combos configured")
		return nil, nil
	}

	telemetry := sc.GetTelemetry(sc.config.TelemetryWindowMin)

	switch goal {
	case GoalAuto:
		return sc.resolveAuto(combos, telemetry), nil
	case GoalEconomy:
		return sc.findByGoalWithFallback(combos, "economy", []string{"balanced", "premium"}), nil
	case GoalPremium:
		return sc.findByGoalWithFallback(combos, "premium", []string{"balanced", "economy"}), nil
	default: // balanced
		return sc.findByGoalWithFallback(combos, "balanced", []string{"economy", "premium"}), nil
	}
}

// resolveAuto dynamically picks a goal based on telemetry.
// Thresholds use rate-normalized values so they stay meaningful regardless of
// the telemetry window.
func (sc *SmartCombo) resolveAuto(combos []*db.Combo, telemetry *Telemetry) *db.Combo {
	if telemetry == nil || telemetry.WindowMin <= 0 {
		logging.Logger.Info("smart combo: no telemetry data, defaulting to balanced")
		return sc.findByGoalWithFallback(combos, "balanced", []string{"economy", "premium"})
	}

	costPerMinute := telemetry.TotalCost / float64(telemetry.WindowMin)

	// Log telemetry for debugging
	logging.Logger.Info("smart combo telemetry",
		"error_rate", telemetry.ErrorRate,
		"error_threshold", sc.config.ErrorRateThreshold,
		"cost_per_min", costPerMinute,
		"cost_threshold", sc.config.CostPerMinThreshold,
		"total_reqs", telemetry.TotalReqs,
		"errors", telemetry.ErrorCount,
		"window_min", telemetry.WindowMin,
	)

	// High error rate → escalate to premium
	if telemetry.ErrorRate >= sc.config.ErrorRateThreshold {
		logging.Logger.Info("smart combo: high error rate, escalating to premium",
			"error_rate", telemetry.ErrorRate,
			"threshold", sc.config.ErrorRateThreshold,
		)
		if c := sc.findByGoalWithFallback(combos, "premium", []string{"balanced", "economy"}); c != nil {
			return c
		}
	}

	// High burn rate → shift to economy
	if costPerMinute >= sc.config.CostPerMinThreshold {
		logging.Logger.Info("smart combo: high burn rate, shifting to economy",
			"cost_per_min", costPerMinute,
			"threshold", sc.config.CostPerMinThreshold,
		)
		if c := sc.findByGoalWithFallback(combos, "economy", []string{"balanced", "premium"}); c != nil {
			return c
		}
	}

	// Default: balanced
	logging.Logger.Debug("smart combo: defaulting to balanced")
	return sc.findByGoalWithFallback(combos, "balanced", []string{"economy", "premium"})
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

// findByGoalWithFallback tries the primary goal, then falls back to alternatives.
func (sc *SmartCombo) findByGoalWithFallback(combos []*db.Combo, primary string, fallbacks []string) *db.Combo {
	if c := sc.findByGoal(combos, primary); c != nil {
		return c
	}
	for _, fb := range fallbacks {
		if c := sc.findByGoal(combos, fb); c != nil {
			logging.Logger.Info("smart combo: primary goal not found, using fallback",
				"primary", primary,
				"fallback", fb,
			)
			return c
		}
	}
	// Last resort: return first available combo
	if len(combos) > 0 {
		logging.Logger.Warn("smart combo: no matching goal found, using first available",
			"requested", primary,
			"fallback", combos[0].Name,
		)
		return combos[0]
	}
	return nil
}

// GetTelemetry computes recent telemetry from request logs with in-memory caching.
func (sc *SmartCombo) GetTelemetry(minutes int) *Telemetry {
	if minutes <= 0 {
		minutes = sc.config.TelemetryWindowMin
	}

	cacheTTL := time.Duration(sc.config.CacheTTLSec) * time.Second

	sc.mu.RLock()
	if sc.cachedTelemetry != nil && time.Since(sc.cachedAt) < cacheTTL {
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

	tel := &Telemetry{WindowMin: minutes, TotalReqs: total, ErrorCount: errors}
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

// GetConfig returns the current configuration (for debugging/display).
func (sc *SmartCombo) GetConfig() Config {
	return sc.config
}

// timeNow and timeMinutes are functions so tests can mock them.
var timeNow = func() time.Time { return time.Now() }
var timeMinutes = func(m int) time.Duration { return time.Duration(m) * time.Minute }
