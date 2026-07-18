package backup

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	_ "modernc.org/sqlite"
)

func TestScannerBacksUpSelectedCategoryTablesAsNDJSON(t *testing.T) {
	database := openBackupScannerTestDB(t)
	mustExec(t, database, `CREATE TABLE provider_types (id TEXT PRIMARY KEY, display_name TEXT NOT NULL, created_at INTEGER NOT NULL)`)
	mustExec(t, database, `CREATE TABLE connections (id TEXT PRIMARY KEY, provider_type_id TEXT NOT NULL, name TEXT NOT NULL, created_at INTEGER NOT NULL)`)
	mustExec(t, database, `CREATE TABLE combos (id TEXT PRIMARY KEY, name TEXT NOT NULL)`)
	mustExec(t, database, `INSERT INTO provider_types (id, display_name, created_at) VALUES ('openai', 'OpenAI Platform', 101)`)
	mustExec(t, database, `INSERT INTO connections (id, provider_type_id, name, created_at) VALUES ('conn-1', 'openai', 'Primary', 202)`)
	mustExec(t, database, `INSERT INTO combos (id, name) VALUES ('combo-1', 'Combo One')`)

	var out bytes.Buffer
	scanner := NewScanner(database)
	if err := scanner.Backup(context.Background(), &out, []string{"providers"}, ""); err != nil {
		t.Fatalf("Backup returned error: %v", err)
	}

	header, rows := decodeBackupLines(t, out.Bytes())
	if header.Format != FormatName {
		t.Fatalf("header format = %q, want %q", header.Format, FormatName)
	}
	if header.Version != FormatVersion {
		t.Fatalf("header version = %d, want %d", header.Version, FormatVersion)
	}
	if got, want := header.Categories, []string{"providers"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("header categories = %#v, want %#v", got, want)
	}
	if len(rows) != 3 {
		t.Fatalf("row count = %d, want 3: %#v", len(rows), rows)
	}
	assertRow(t, rows, "provider_types", map[string]any{
		"id":           "openai",
		"display_name": "OpenAI Platform",
		"created_at":   float64(101),
	})
	assertRow(t, rows, "connections", map[string]any{
		"id":              "conn-1",
		"provider_type_id": "openai",
		"name":            "Primary",
		"created_at":      float64(202),
	})
	assertRow(t, rows, "combos", map[string]any{
		"id":   "combo-1",
		"name": "Combo One",
	})
}

func TestScannerBacksUpOnlySelectedCategory(t *testing.T) {
	database := openBackupScannerTestDB(t)
	mustExec(t, database, `CREATE TABLE provider_types (id TEXT PRIMARY KEY, name TEXT NOT NULL)`)
	mustExec(t, database, `CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`)
	mustExec(t, database, `INSERT INTO provider_types (id, name) VALUES ('openai', 'OpenAI')`)
	mustExec(t, database, `INSERT INTO settings (key, value) VALUES ('theme', 'dark')`)

	var out bytes.Buffer
	if err := NewScanner(database).Backup(context.Background(), &out, []string{"config"}, ""); err != nil {
		t.Fatalf("Backup returned error: %v", err)
	}

	_, rows := decodeBackupLines(t, out.Bytes())
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1: %#v", len(rows), rows)
	}
	assertRow(t, rows, "settings", map[string]any{
		"key":   "theme",
		"value": "dark",
	})
}

func TestScannerSkipsMissingTablesInSelectedCategory(t *testing.T) {
	database := openBackupScannerTestDB(t)
	mustExec(t, database, `CREATE TABLE combos (id TEXT PRIMARY KEY, name TEXT NOT NULL)`)
	mustExec(t, database, `INSERT INTO combos (id, name) VALUES ('combo-1', 'Combo One')`)

	var out bytes.Buffer
	if err := NewScanner(database).Backup(context.Background(), &out, []string{"providers"}, ""); err != nil {
		t.Fatalf("Backup returned error: %v", err)
	}

	_, rows := decodeBackupLines(t, out.Bytes())
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1: %#v", len(rows), rows)
	}
	assertRow(t, rows, "combos", map[string]any{
		"id":   "combo-1",
		"name": "Combo One",
	})
}

func TestScannerRejectsUnknownCategory(t *testing.T) {
	database := openBackupScannerTestDB(t)
	var out bytes.Buffer
	if err := NewScanner(database).Backup(context.Background(), &out, []string{"unknown"}, ""); err == nil {
		t.Fatal("Backup returned nil error for unknown category")
	}
}

func openBackupScannerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func mustExec(t *testing.T, database *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func decodeBackupLines(t *testing.T, payload []byte) (Header, []Row) {
	t.Helper()
	scanner := bufio.NewScanner(bytes.NewReader(payload))
	if !scanner.Scan() {
		t.Fatal("missing backup header")
	}
	var header Header
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		t.Fatalf("decode header: %v", err)
	}
	var rows []Row
	for scanner.Scan() {
		var row Row
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatalf("decode row: %v", err)
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan backup payload: %v", err)
	}
	return header, rows
}

func assertRow(t *testing.T, rows []Row, table string, data map[string]any) {
	t.Helper()
	for _, row := range rows {
		if row.Table != table {
			continue
		}
		if len(row.Data) != len(data) {
			t.Fatalf("%s data length = %d, want %d: %#v", table, len(row.Data), len(data), row.Data)
		}
		for key, want := range data {
			if got := row.Data[key]; got != want {
				t.Fatalf("%s[%s] = %#v, want %#v", table, key, got, want)
			}
		}
		return
	}
	t.Fatalf("missing row for table %s in %#v", table, rows)
}
