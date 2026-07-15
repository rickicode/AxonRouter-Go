package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
)

// TestMimocodeCanonicalMigration verifies the legacy `mimocode-free` provider id
// is normalized to the canonical `mimocode` alias, so the dashboard does not show
// two MiMoCode Free providers.
func TestMimocodeCanonicalMigration(t *testing.T) {
	dir := t.TempDir()
	d, err := sql.Open("sqlite", filepath.Join(dir, "verify.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	// Simulate a pre-existing deployment: a legacy `mimocode-free` provider_type + connection.
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS provider_types (id TEXT PRIMARY KEY, display_name TEXT NOT NULL, format TEXT NOT NULL, base_url TEXT NOT NULL, is_custom INTEGER DEFAULT 0, custom_headers TEXT, created_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS connections (id TEXT PRIMARY KEY, provider_type_id TEXT NOT NULL REFERENCES provider_types(id), name TEXT NOT NULL, auth_type TEXT NOT NULL, provider_specific_data TEXT, status TEXT NOT NULL DEFAULT 'ready', is_active INTEGER DEFAULT 1, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('mimocode-free','MiMoCode Free Tier','openai','https://api.xiaomimimo.com/api/free-ai/openai',0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, created_at, updated_at) VALUES ('conn-legacy','mimocode-free','legacy','none',0,0)`); err != nil {
		t.Fatal(err)
	}

	// Run migrations (idempotent): seed mimocode, rename legacy conn, drop mimocode-free.
	if err := RunMigrations(d); err != nil {
		t.Fatal(err)
	}

	var hasMimocode, hasLegacy bool
	rows, err := d.Query("SELECT id FROM provider_types WHERE id IN ('mimocode','mimocode-free')")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		if id == "mimocode" {
			hasMimocode = true
		}
		if id == "mimocode-free" {
			hasLegacy = true
		}
	}
	rows.Close()
	if !hasMimocode {
		t.Errorf("provider_type %q missing after migration", "mimocode")
	}
	if hasLegacy {
		t.Errorf("legacy provider_type %q should have been removed", "mimocode-free")
	}

	var pt string
	if err := d.QueryRow("SELECT provider_type_id FROM connections WHERE id='conn-legacy'").Scan(&pt); err != nil {
		t.Fatal(err)
	}
	if pt != "mimocode" {
		t.Errorf("conn-legacy provider_type_id = %q, want mimocode", pt)
	}

	var psdStr string
	if err := d.QueryRow("SELECT provider_specific_data FROM connections WHERE id='mimocode-direct-default'").Scan(&psdStr); err != nil {
		t.Fatalf("mimocode-direct-default missing after migration: %v", err)
	}
	var psd map[string]interface{}
	if err := json.Unmarshal([]byte(psdStr), &psd); err != nil {
		t.Fatalf("mimocode-direct-default provider_specific_data is invalid JSON: %v", err)
	}
	if psd["direct"] != "true" {
		t.Errorf("provider_specific_data.direct = %v, want 'true'", psd["direct"])
	}
	if psd["accountId"] != "mimocode-default" {
		t.Errorf("provider_specific_data.accountId = %v, want 'mimocode-default'", psd["accountId"])
	}
	if lbl, ok := psd["accountLabel"].(string); !ok || lbl == "" {
		t.Errorf("provider_specific_data.accountLabel invalid: %v", psd["accountLabel"])
	}
	if fp, ok := psd["fingerprint"].(string); !ok || len(fp) != 64 {
		t.Errorf("provider_specific_data.fingerprint length = %d, want 64", len(fmt.Sprint(psd["fingerprint"])))
	}
}
