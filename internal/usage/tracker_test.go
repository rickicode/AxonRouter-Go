package usage

import (
	"database/sql"
	"math"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/rickicode/AxonRouter-Go/internal/db"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "tracker-test.db")
	database, err := sql.Open("sqlite", tmp)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestTracker_DropsWhenFull(t *testing.T) {
	database := openTestDB(t)

	// Tiny buffer to force drops.
	tracker := &Tracker{
		buffer:      make(chan *LogEntry, 1),
		db:          database,
		flushTicker: time.NewTicker(time.Hour),
		batchSize:   100,
		stopCh:      make(chan struct{}),
	}
	defer tracker.Stop()

	tracker.Log(&LogEntry{ConnectionID: "c1"})
	tracker.Log(&LogEntry{ConnectionID: "c2"}) // should be dropped

	if got := tracker.Buffered(); got != 1 {
		t.Errorf("expected 1 buffered, got %d", got)
	}
	if got := tracker.Dropped(); got != 1 {
		t.Errorf("expected 1 dropped, got %d", got)
	}
}

func TestTracker_Flush(t *testing.T) {
	database := openTestDB(t)

	tracker := NewTracker(database)
	defer tracker.Stop()

	tracker.Log(&LogEntry{
		ConnectionID:   "flush-c1",
		ProviderTypeID: "openai",
		ModelID:        "gpt-4o",
		Modality:       "chat",
	})

	// Wait for the default 5s flush interval.
	time.Sleep(6 * time.Second)

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM request_logs WHERE connection_id = ?`, "flush-c1").Scan(&count); err != nil {
		t.Fatalf("query logs: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 log row, got %d", count)
	}
}

func TestTracker_PersistProxyPoolID(t *testing.T) {
	database := openTestDB(t)

	tracker := NewTracker(database)
	defer tracker.Stop()

	tracker.Log(&LogEntry{
		ConnectionID: "conn-pool",
		ProxyPoolID:  "pool-1",
		Modality:     "chat",
	})

	time.Sleep(6 * time.Second)

	var poolID string
	if err := database.QueryRow(`SELECT proxy_pool_id FROM request_logs WHERE connection_id = ?`, "conn-pool").Scan(&poolID); err != nil {
		t.Fatalf("query proxy_pool_id: %v", err)
	}
	if poolID != "pool-1" {
		t.Errorf("expected proxy_pool_id pool-1, got %q", poolID)
	}
}

func TestTracker_PersistServiceTierAndMultiplier(t *testing.T) {
	database := openTestDB(t)

	tracker := NewTracker(database)
	defer tracker.Stop()

	tracker.Log(&LogEntry{
		ConnectionID:   "conn-tier",
		ProviderTypeID: "cx",
		ModelID:        "openai/gpt-5-codex",
		ServiceTier:    "fast",
		InputTokens:    1000,
		OutputTokens:   1000,
		Modality:       "chat",
	})

	time.Sleep(6 * time.Second)

	var serviceTier string
	var cost float64
	if err := database.QueryRow(`SELECT service_tier, cost_usd FROM request_logs WHERE connection_id = ?`, "conn-tier").Scan(&serviceTier, &cost); err != nil {
		t.Fatalf("query service_tier: %v", err)
	}
	if serviceTier != "fast" {
		t.Errorf("expected service_tier fast, got %q", serviceTier)
	}

	p := GetPricing("gpt-5-codex")
	base := float64(1000)/1000*p.InputPer1K + float64(1000)/1000*p.OutputPer1K
	want := base * p.TierFastMultiplier
	if math.Abs(cost-want) > 1e-9 {
		t.Errorf("cost = %.6f, want %.6f", cost, want)
	}
}
