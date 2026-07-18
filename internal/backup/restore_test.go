package backup

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	appdb "github.com/rickicode/AxonRouter-Go/internal/db"

	_ "modernc.org/sqlite"
)

func TestRestoreToSQLiteTargetMatchesBackedUpRowCounts(t *testing.T) {
	ctx := context.Background()
	source := openBackupTestDB(t)
	seedBackupTestData(t, source)

	var backup bytes.Buffer
	if err := NewScanner(source).Backup(ctx, &backup, []string{"providers", "api_keys"}, ""); err != nil {
		t.Fatalf("backup source: %v", err)
	}

	targetPath := filepath.Join(t.TempDir(), "restored.db")
	result, err := Restore(ctx, bytes.NewReader(backup.Bytes()), RestoreOptions{
		Target:     RestoreTargetSQLite,
		SQLitePath: targetPath,
	})
	if err != nil {
		t.Fatalf("restore backup: %v", err)
	}
	if result.RestartRequired {
		t.Fatal("sqlite target should not require process restart")
	}
	if result.RowsRestored == 0 {
		t.Fatal("expected restored rows")
	}

	target := openSQLiteFile(t, targetPath)
	defer target.Close()
	for _, table := range []string{"provider_types", "connections", "api_keys", "combos", "combo_steps"} {
		want := countRows(t, source, table)
		got := countRows(t, target, table)
		if got != want {
			t.Fatalf("%s row count: got %d want %d", table, got, want)
		}
	}
}

func TestRestoreCurrentTargetPausesWriteQueueAndWarnsRestart(t *testing.T) {
	ctx := context.Background()
	source := openBackupTestDB(t)
	seedBackupTestData(t, source)

	var backup bytes.Buffer
	if err := NewScanner(source).Backup(ctx, &backup, []string{"providers"}, ""); err != nil {
		t.Fatalf("backup source: %v", err)
	}

	target := openBackupTestDB(t)
	queue := appdb.NewWriteQueue(target)
	defer queue.Stop()

	result, err := Restore(ctx, bytes.NewReader(backup.Bytes()), RestoreOptions{
		Target:     RestoreTargetCurrent,
		CurrentDB:  target,
		WriteQueue: queue,
	})
	if err != nil {
		t.Fatalf("restore current backup: %v", err)
	}
	if !result.RestartRequired {
		t.Fatal("current target should require process restart")
	}
	if result.Warning == "" {
		t.Fatal("current target should include restart warning")
	}
	if got, want := countRows(t, target, "connections"), countRows(t, source, "connections"); got != want {
		t.Fatalf("connections row count: got %d want %d", got, want)
	}
}

func openBackupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d := openSQLiteFile(t, filepath.Join(t.TempDir(), "test.db"))
	if err := appdb.RunMigrations(d); err != nil {
		d.Close()
		t.Fatalf("run migrations: %v", err)
	}
	return d
}

func openSQLiteFile(t *testing.T, path string) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func seedBackupTestData(t *testing.T, d *sql.DB) {
	t.Helper()
	now := int64(1700000000)
	mustExecRestore(t, d, `INSERT OR REPLACE INTO provider_types (id, display_name, format, base_url, is_custom, category, service_kinds, created_at) VALUES ('restore-provider', 'Restore Provider', 'openai', 'https://example.test/v1', 1, 'apikey', '["llm"]', ?)`, now)
	mustExecRestore(t, d, `INSERT OR REPLACE INTO connections (id, provider_type_id, name, auth_type, api_key, status, is_active, created_at, updated_at) VALUES ('restore-conn', 'restore-provider', 'Restore Conn', 'api_key', 'secret', 'ready', 1, ?, ?)`, now, now)
	mustExecRestore(t, d, `INSERT OR REPLACE INTO api_keys (id, key_hash, name, rate_limit_per_min, is_active, created_at) VALUES ('restore-key', 'hash-restore', 'Restore Key', 60, 1, ?)`, now)
	mustExecRestore(t, d, `INSERT OR REPLACE INTO combos (id, name, strategy, created_at, updated_at) VALUES ('restore-combo', 'Restore Combo', 'priority', ?, ?)`, now, now)
	mustExecRestore(t, d, `INSERT OR REPLACE INTO combo_steps (id, combo_id, connection_id, model_id, priority, created_at) VALUES ('restore-step', 'restore-combo', 'restore-conn', 'gpt-test', 1, ?)`, now)
}

func mustExecRestore(t *testing.T, d *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := d.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func countRows(t *testing.T, d *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := d.QueryRow("SELECT COUNT(*) FROM " + quoteIdentifier(table)).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}
