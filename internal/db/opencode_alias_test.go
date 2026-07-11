package db

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
)

// TestOpenCodeCanonicalMigration verifies the OpenCode Free provider id is
// normalized to the short alias `oc` (matching cx/cf), that OpenCode Zen/Go are
// seeded as distinct providers, and that a pre-existing `opencode` connection is
// migrated without being orphaned.
func TestOpenCodeCanonicalMigration(t *testing.T) {
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "verify.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// Simulate a pre-existing deployment: an `opencode` provider_type + connection.
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS provider_types (id TEXT PRIMARY KEY, display_name TEXT NOT NULL, format TEXT NOT NULL, base_url TEXT NOT NULL, is_custom INTEGER DEFAULT 0, custom_headers TEXT, created_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS connections (id TEXT PRIMARY KEY, provider_type_id TEXT NOT NULL REFERENCES provider_types(id), name TEXT NOT NULL, auth_type TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'ready', is_active INTEGER DEFAULT 1, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('opencode','OpenCode Free','openai','https://opencode.ai/zen/v1',0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, created_at, updated_at) VALUES ('conn-old','opencode','old','none',0,0)`); err != nil {
		t.Fatal(err)
	}

	// Run migrations (idempotent): seed oc/oc-zen/oc-go, rename conn-old, drop opencode.
	if err := RunMigrations(d); err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{"oc": true, "oc-zen": true, "oc-go": true, "opencode": false}
	rows, err := d.Query("SELECT id FROM provider_types WHERE id IN ('oc','oc-zen','oc-go','opencode')")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		got[id] = true
	}
	rows.Close()
	for id, wantPresent := range want {
		if wantPresent && !got[id] {
			t.Errorf("provider_type %q missing after migration", id)
		}
		if !wantPresent && got[id] {
			t.Errorf("legacy provider_type %q should have been removed", id)
		}
	}

	var pt string
	if err := d.QueryRow("SELECT provider_type_id FROM connections WHERE id='conn-old'").Scan(&pt); err != nil {
		t.Fatal(err)
	}
	if pt != "oc" {
		t.Errorf("conn-old provider_type_id = %q, want oc", pt)
	}
}
