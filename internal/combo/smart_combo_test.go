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
