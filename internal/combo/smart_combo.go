package combo

import (
	"database/sql"
	"math"
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
}

// SmartCombo resolves which combo to use based on a goal.
type SmartCombo struct {
	mu sync.RWMutex
	db *sql.DB
}

// NewSmartCombo creates a new smart combo resolver.
func NewSmartCombo(database *sql.DB) *SmartCombo {
	return &SmartCombo{db: database}
}

// Resolve selects the best combo for the given goal.
func (sc *SmartCombo) Resolve(goal SmartGoal, telemetry *Telemetry) (*db.Combo, error) {
	combos, err := sc.getSmartCombos()
	if err != nil {
		return nil, err
	}
	if len(combos) == 0 {
		return nil, nil
	}

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
func (sc *SmartCombo) resolveAuto(combos []*db.Combo, telemetry *Telemetry) *db.Combo {
	if telemetry == nil {
		return sc.findByGoal(combos, "balanced")
	}
	// High error rate → escalate to premium
	if telemetry.ErrorRate >= 0.15 || telemetry.FallbackRate >= 0.2 {
		if c := sc.findByGoal(combos, "premium"); c != nil {
			return c
		}
	}
	// High cost → shift to economy
	if telemetry.TotalCost >= 50 {
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

// getSmartCombos loads all active smart combos from the database.
func (sc *SmartCombo) getSmartCombos() ([]*db.Combo, error) {
	rows, err := sc.db.Query(`
		SELECT id, name, strategy, sticky_limit, timeout_ms, is_smart, smart_goal,
		       is_active, created_at, updated_at
		FROM combos WHERE is_smart = 1 AND is_active = 1
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var combos []*db.Combo
	for rows.Next() {
		c := &db.Combo{}
		err := rows.Scan(&c.ID, &c.Name, &c.Strategy, &c.StickyLimit,
			&c.TimeoutMs, &c.IsSmart, &c.SmartGoal,
			&c.IsActive, &c.CreatedAt, &c.UpdatedAt)
		if err != nil {
			continue
		}
		combos = append(combos, c)
	}
	return combos, nil
}

// GetTelemetry computes recent telemetry from request logs.
func (sc *SmartCombo) GetTelemetry(minutes int) *Telemetry {
	if minutes <= 0 {
		minutes = 60
	}
	var total, errors int64
	var cost float64
	var latencySum int64

	since := timeNow().Add(-timeMinutes(minutes)).Unix()
	sc.db.QueryRow(`
		SELECT COUNT(*), 
		       SUM(CASE WHEN error_message IS NOT NULL AND error_message != '' THEN 1 ELSE 0 END),
		       SUM(cost_usd),
		       COALESCE(SUM(latency_ms), 0)
		FROM request_logs WHERE timestamp > ?
	`, since).Scan(&total, &errors, &cost, &latencySum)

	if total == 0 {
		return &Telemetry{}
	}
	return &Telemetry{
		ErrorRate:    float64(errors) / float64(total),
		TotalCost:    cost,
		AvgLatency:   float64(latencySum) / float64(total),
	}
}

// EstimateCost scores a combo by average model pricing (lower = cheaper).
func EstimateCost(combo *db.Combo, steps []db.ComboStep) float64 {
	// ponytail: simple — sum of step costs, no weighting
	total := 0.0
	for _, s := range steps {
		total += getModelCostPer1K(s.ModelID)
	}
	return total / math.Max(1, float64(len(steps)))
}

func getModelCostPer1K(modelID string) float64 {
	// ponytail: hardcoded lookup, upgrade to DB pricing table later
	switch {
	case contains(modelID, "gpt-4o"):
		return 0.005
	case contains(modelID, "gpt-4"):
		return 0.03
	case contains(modelID, "claude-sonnet"):
		return 0.003
	case contains(modelID, "mimo"):
		return 0.001
	default:
		return 0.002
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && (s[0:len(sub)] == sub || contains(s[1:], sub)))
}

// timeNow and timeMinutes are functions so tests can mock them.
var timeNow = func() time.Time { return time.Now() }
var timeMinutes = func(m int) time.Duration { return time.Duration(m) * time.Minute }
