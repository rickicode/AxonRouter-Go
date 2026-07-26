package background

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newAutoPingTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	schema := `
CREATE TABLE IF NOT EXISTS settings (
	key TEXT PRIMARY KEY,
	value TEXT,
	updated_at INTEGER
);
CREATE TABLE IF NOT EXISTS provider_types (
	id TEXT PRIMARY KEY,
	base_url TEXT
);
CREATE TABLE IF NOT EXISTS connections (
	id TEXT PRIMARY KEY,
	provider_type_id TEXT,
	auth_type TEXT,
	is_active INTEGER,
	api_key TEXT,
	oauth_token TEXT,
	provider_specific_data TEXT,
	updated_at INTEGER
);
CREATE TABLE IF NOT EXISTS quota_cache (
	connection_id TEXT,
	provider_type_id TEXT,
	quotas TEXT,
	fetched_at INTEGER,
	updated_at INTEGER
);
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAutoPingScheduler_loadSettingsDefaultsAndStoredValues(t *testing.T) {
	db := newAutoPingTestDB(t)
	defer db.Close()

	s := NewAutoPingScheduler(db, 1)

	stored := autoPingSettings{Enabled: true, Connections: map[string]bool{"c1": true}}
	data, _ := json.Marshal(stored)
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, claudeAutoPingSettingKey, string(data)); err != nil {
		t.Fatal(err)
	}

	settings := s.loadSettings()
	if !settings[claudeAutoPingSettingKey].Enabled {
		t.Errorf("expected Claude auto-ping enabled")
	}
	if !settings[claudeAutoPingSettingKey].Connections["c1"] {
		t.Errorf("expected connection c1 enabled")
	}
	if settings[codexAutoPingSettingKey].Connections == nil {
		t.Errorf("expected empty codex connections map, got nil")
	}
}

func TestAutoPingScheduler_pingIfDue_resetsAndPingsOnQuotaAdvance(t *testing.T) {
	db := newAutoPingTestDB(t)
	defer db.Close()

	if _, err := db.Exec(`INSERT INTO provider_types (id, base_url) VALUES (?, ?)`, "claude", "https://api.anthropic.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO connections (id, provider_type_id, auth_type, is_active, api_key, updated_at) VALUES (?, ?, 'oauth', 1, 'key', ?)`,
		"conn-1", "claude", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	cfg := autoPingProviderConfigs["claude"]
	stored := autoPingSettings{Enabled: true, Connections: map[string]bool{"conn-1": true}}
	data, _ := json.Marshal(stored)
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, cfg.SettingKey, string(data)); err != nil {
		t.Fatal(err)
	}

	s := NewAutoPingScheduler(db, 1)
	s.loadMetrics()

	reset1 := time.Now().Add(-time.Hour).Unix()
	quotas, _ := json.Marshal([]map[string]string{{"reset_at": time.Unix(reset1, 0).Format(time.RFC3339)}})
	if _, err := db.Exec(`INSERT INTO quota_cache (connection_id, provider_type_id, quotas, fetched_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"conn-1", "claude", string(quotas), time.Now().Unix(), time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	called := false
	s.sender = func(ctx context.Context, c autoPingProviderConfig, conn autoPingConnection) (int, error) {
		called = true
		return 200, nil
	}

	// First check should only record the reset, not ping.
	now := time.Now().Unix()
	s.pingIfDue(now, cfg, autoPingConnection{ID: "conn-1", ProviderTypeID: "claude", APIKey: "key"})
	if called {
		t.Fatal("expected no ping on first reset observation")
	}

	// Advance the reset timestamp.
	reset2 := time.Now().Add(time.Minute).Unix()
	quotas2, _ := json.Marshal([]map[string]string{{"reset_at": time.Unix(reset2, 0).Format(time.RFC3339)}})
	if _, err := db.Exec(`UPDATE quota_cache SET quotas = ?, fetched_at = ?, updated_at = ? WHERE connection_id = ?`,
		string(quotas2), time.Now().Unix(), time.Now().Unix(), "conn-1"); err != nil {
		t.Fatal(err)
	}

	s.pingIfDue(now, cfg, autoPingConnection{ID: "conn-1", ProviderTypeID: "claude", APIKey: "key"})
	if !called {
		t.Fatal("expected ping after reset advanced")
	}
}

func TestAutoPingScheduler_pingIfDue_fallbackInterval(t *testing.T) {
	db := newAutoPingTestDB(t)
	defer db.Close()

	if _, err := db.Exec(`INSERT INTO provider_types (id, base_url) VALUES (?, ?)`, "cx", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO connections (id, provider_type_id, auth_type, is_active, oauth_token, updated_at) VALUES (?, ?, 'oauth', 1, 'tok', ?)`,
		"conn-cx", "cx", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	cfg := autoPingProviderConfigs["cx"]
	stored := autoPingSettings{Enabled: true, Connections: map[string]bool{"conn-cx": true}}
	data, _ := json.Marshal(stored)
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)`, cfg.SettingKey, string(data)); err != nil {
		t.Fatal(err)
	}

	s := NewAutoPingScheduler(db, 1)
	s.sender = func(ctx context.Context, c autoPingProviderConfig, conn autoPingConnection) (int, error) {
		return 200, nil
	}

	now := time.Now().Unix()
	m := s.metrics.provider("cx")
	cm := m.connMetrics("conn-cx")
	cm.LastPingAt = now - int64(defaultAutoPingFallbackIntervalMin*60) - 1

	if !s.pingIfDue(now, cfg, autoPingConnection{ID: "conn-cx", ProviderTypeID: "cx", AccessToken: "tok"}) {
		t.Fatal("expected ping due to fallback interval")
	}
	if s.metrics.provider("cx").Count != 1 {
		t.Fatalf("expected ping count 1, got %d", s.metrics.provider("cx").Count)
	}
}

func TestAutoPingScheduler_saveAndLoadMetrics(t *testing.T) {
	db := newAutoPingTestDB(t)
	defer db.Close()

	s := NewAutoPingScheduler(db, 1)
	s.metrics.provider("claude").Count = 42
	s.dirty = true
	s.saveMetrics()

	s2 := NewAutoPingScheduler(db, 1)
	s2.loadMetrics()
	if s2.metrics.provider("claude").Count != 42 {
		t.Fatalf("expected metrics count 42 after reload, got %d", s2.metrics.provider("claude").Count)
	}
}
