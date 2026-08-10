package background

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/db"

	_ "modernc.org/sqlite"
)

func openLifecycleTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "lifecycle-test.db")
	database, err := sql.Open("sqlite", tmp)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := database.Exec("PRAGMA journal_mode=WAL"); err != nil {
		t.Fatalf("wal mode: %v", err)
	}
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func seedProviderType(t *testing.T, database *sql.DB, id string) {
	t.Helper()
	now := time.Now().Unix()
	if _, err := database.Exec(
		`INSERT INTO provider_types (id, display_name, format, base_url, is_custom, created_at) VALUES (?, ?, ?, ?, 1, ?)`,
		id, id, "openai", "http://localhost", now,
	); err != nil {
		t.Fatalf("seed provider type: %v", err)
	}
}

func insertConnection(t *testing.T, database *sql.DB, id, providerID, status string, isActive bool, updatedAt int64) {
	t.Helper()
	now := time.Now().Unix()
	active := 0
	if isActive {
		active = 1
	}
	if _, err := database.Exec(
		`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, providerID, id, "none", status, active, now, updatedAt,
	); err != nil {
		t.Fatalf("insert connection %s: %v", id, err)
	}
}

func TestLifecycleCleanupDeletesOldLegacyTerminal(t *testing.T) {
	database := openLifecycleTestDB(t)
	seedProviderType(t, database, "test")

	cutoff := time.Now().Add(-8 * 24 * time.Hour).Unix()
	insertConnection(t, database, "conn-auth-old", "test", "auth_failed", false, cutoff)
	insertConnection(t, database, "conn-suspended-old", "test", "suspended", false, cutoff)
	insertConnection(t, database, "conn-balance-old", "test", "balance_empty", false, cutoff)
	// Canonical disabled rows must NEVER be auto-deleted.
	insertConnection(t, database, "conn-disabled-old", "test", "disabled", false, cutoff)

	lm := NewLifecycleManager(database, 60)
	deleted, err := lm.Cleanup()
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("deleted = %d, want 3", deleted)
	}
}

func TestLifecycleCleanupKeepsRecentRecords(t *testing.T) {
	database := openLifecycleTestDB(t)
	seedProviderType(t, database, "test")

	recent := time.Now().Add(-1 * time.Hour).Unix()
	insertConnection(t, database, "conn-auth-recent", "test", "auth_failed", false, recent)
	insertConnection(t, database, "conn-disabled-recent", "test", "disabled", false, recent)

	lm := NewLifecycleManager(database, 60)
	deleted, err := lm.Cleanup()
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d, want 0", deleted)
	}
}

func TestLifecycleCleanupKeepsActiveRecords(t *testing.T) {
	database := openLifecycleTestDB(t)
	seedProviderType(t, database, "test")

	old := time.Now().Add(-8 * 24 * time.Hour).Unix()
	insertConnection(t, database, "conn-active-old", "test", "disabled", true, old)

	lm := NewLifecycleManager(database, 60)
	deleted, err := lm.Cleanup()
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d, want 0", deleted)
	}
}

func TestLifecycleCleanupKeepsOtherStatuses(t *testing.T) {
	database := openLifecycleTestDB(t)
	seedProviderType(t, database, "test")

	old := time.Now().Add(-8 * 24 * time.Hour).Unix()
	insertConnection(t, database, "conn-ready-old", "test", "ready", false, old)

	lm := NewLifecycleManager(database, 60)
	deleted, err := lm.Cleanup()
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("deleted = %d, want 0", deleted)
	}
}

func TestLifecycleManagerStopIsIdempotent(t *testing.T) {
	lm := NewLifecycleManager(nil, 60)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lm.Start(ctx)
	lm.Stop()
	lm.Stop()
}
