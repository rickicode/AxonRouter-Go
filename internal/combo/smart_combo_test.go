package combo

import (
	"database/sql"
	"math"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/rickicode/AxonRouter-Go/internal/db"
)

// TestGetTelemetryWindowUsesMilliseconds guards against a unit bug where
// `since` was computed with .Unix() (seconds) while request_logs.timestamp is
// stored in milliseconds (tracker.go sets UnixMilli()). That mismatch made
// `WHERE timestamp > since` always true, aggregating ALL history and breaking
// combo economy-switch. Recent (inside window) must be counted; old must not.
func TestGetTelemetryWindowUsesMilliseconds(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "telemetry-test.db")
	database, err := sql.Open("sqlite", tmp)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	base := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	recentTs := base.UnixMilli()
	oldTs := base.Add(-90 * time.Minute).UnixMilli()

	if _, err := database.Exec(
		`INSERT INTO request_logs (id, timestamp, modality, status_code, cost_usd, model_id, created_at, input_tokens, output_tokens)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		"recent", recentTs, "chat", 200, 5.0, "gpt-4o", recentTs, 10, 20,
	); err != nil {
		t.Fatalf("insert recent: %v", err)
	}
	if _, err := database.Exec(
		`INSERT INTO request_logs (id, timestamp, modality, status_code, cost_usd, model_id, created_at, input_tokens, output_tokens)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		"old", oldTs, "chat", 200, 100.0, "gpt-4o", oldTs, 10, 20,
	); err != nil {
		t.Fatalf("insert old: %v", err)
	}

	origNow := timeNow
	timeNow = func() time.Time { return base }
	t.Cleanup(func() { timeNow = origNow })

	sc := NewSmartCombo(database)
	tel := sc.GetTelemetry(60) // 60-minute window; old row is 90 min ago

	if math.Abs(tel.TotalCost-5.0) > 1e-9 {
		t.Fatalf("expected only recent row (cost 5.0), got TotalCost=%v (window filter broken)", tel.TotalCost)
	}
}

func combo(name, goal string) *db.Combo {
	return &db.Combo{
		Name:      name,
		SmartGoal: sql.NullString{String: goal, Valid: goal != ""},
		IsSmart:   true,
	}
}

func smartComboWithTelemetry(tel *Telemetry) *SmartCombo {
	sc := NewSmartCombo(nil)
	sc.cachedTelemetry = tel
	sc.cachedAt = timeNow()
	return sc
}

func goalOf(c *db.Combo) string {
	if c == nil || !c.SmartGoal.Valid {
		return ""
	}
	return c.SmartGoal.String
}

func TestResolve_GoalAuto_ErrorRateAbove15_SwitchesToPremium(t *testing.T) {
	sc := smartComboWithTelemetry(&Telemetry{
		WindowMin:  60,
		TotalCost:  10.0,
		ErrorRate:  0.20,
		TotalReqs:  100,
		ErrorCount: 20,
	})
	combos := []*db.Combo{
		combo("z-balanced", "balanced"),
		combo("a-premium", "premium"),
		combo("m-economy", "economy"),
	}
	got, err := sc.Resolve(GoalAuto, combos)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if goalOf(got) != string(GoalPremium) {
		t.Fatalf("expected goal %q, got %q", GoalPremium, goalOf(got))
	}
}

func TestResolve_GoalAuto_CostAbove85_SwitchesToEconomy(t *testing.T) {
	sc := smartComboWithTelemetry(&Telemetry{
		WindowMin:  60,
		TotalCost:  60.0,
		ErrorRate:  0.02,
		TotalReqs:  50,
		ErrorCount: 1,
	})
	combos := []*db.Combo{
		combo("z-balanced", "balanced"),
		combo("a-premium", "premium"),
		combo("m-economy", "economy"),
	}
	got, err := sc.Resolve(GoalAuto, combos)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if goalOf(got) != string(GoalEconomy) {
		t.Fatalf("expected goal %q, got %q", GoalEconomy, goalOf(got))
	}
}

func TestResolve_GoalAuto_Normal_ReturnsBalanced(t *testing.T) {
	sc := smartComboWithTelemetry(&Telemetry{
		WindowMin:  60,
		TotalCost:  12.0,
		ErrorRate:  0.05,
		TotalReqs:  100,
		ErrorCount: 5,
	})
	combos := []*db.Combo{
		combo("a-economy", "economy"),
		combo("m-balanced", "balanced"),
		combo("z-premium", "premium"),
	}
	got, err := sc.Resolve(GoalAuto, combos)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if goalOf(got) != string(GoalBalanced) {
		t.Fatalf("expected goal %q, got %q", GoalBalanced, goalOf(got))
	}
}

func TestResolve_Fallback_WhenGoalNotFound(t *testing.T) {
	sc := smartComboWithTelemetry(&Telemetry{WindowMin: 60})
	combos := []*db.Combo{
		combo("p1", "premium"),
		combo("b1", "balanced"),
	}
	got, err := sc.Resolve(GoalEconomy, combos)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if goalOf(got) != "balanced" {
		t.Fatalf("expected fallback to balanced, got %q", goalOf(got))
	}

	// When both primary and first fallback are missing, use the second fallback.
	combos2 := []*db.Combo{
		combo("e1", "economy"),
		combo("p2", "premium"),
	}
	got2, err := sc.Resolve(GoalBalanced, combos2)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if goalOf(got2) != "economy" {
		t.Fatalf("expected fallback to economy, got %q", goalOf(got2))
	}
}

func TestResolve_Deterministic_SortByName(t *testing.T) {
	sc := smartComboWithTelemetry(&Telemetry{WindowMin: 60})
	combos := []*db.Combo{
		combo("charlie", ""),
		combo("alpha", ""),
		combo("bravo", ""),
	}
	got1, err := sc.Resolve(GoalBalanced, combos)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	// Same set in a different order must produce the same result.
	combos2 := []*db.Combo{
		combo("bravo", ""),
		combo("charlie", ""),
		combo("alpha", ""),
	}
	got2, err := sc.Resolve(GoalBalanced, combos2)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if got1 == nil || got2 == nil {
		t.Fatalf("expected a combo, got nil")
	}
	if got1.Name != "alpha" || got2.Name != "alpha" {
		t.Fatalf("expected deterministic selection of alpha, got %q and %q", got1.Name, got2.Name)
	}
}
