package db

import (
	"database/sql"
	"time"
)

// RunMigrations creates all tables if they don't exist.
func RunMigrations(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS provider_types (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    format TEXT NOT NULL,
    base_url TEXT NOT NULL,
    is_custom INTEGER DEFAULT 0,
    custom_headers TEXT,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS connections (
    id TEXT PRIMARY KEY,
    provider_type_id TEXT NOT NULL REFERENCES provider_types(id),
    name TEXT NOT NULL,
    auth_type TEXT NOT NULL,
    api_key TEXT,
    oauth_token TEXT,
    oauth_refresh_token TEXT,
    oauth_expires_at INTEGER,
    provider_specific_data TEXT,
    status TEXT NOT NULL DEFAULT 'ready',
    cooldown_until INTEGER,
    last_error TEXT,
    last_error_code INTEGER,
    last_success_at INTEGER,
    last_failure_at INTEGER,
    failure_count INTEGER DEFAULT 0,
    capabilities TEXT,
    is_active INTEGER DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS model_rate_limits (
    id TEXT PRIMARY KEY,
    connection_id TEXT NOT NULL REFERENCES connections(id),
    model_id TEXT NOT NULL,
    tpm_remaining INTEGER,
    tpm_limit INTEGER,
    rpm_remaining INTEGER,
    rpm_limit INTEGER,
    cooldown_until INTEGER,
    last_updated_at INTEGER,
    UNIQUE(connection_id, model_id)
);

CREATE TABLE IF NOT EXISTS api_keys (
    id TEXT PRIMARY KEY,
    key_hash TEXT NOT NULL UNIQUE,
    name TEXT,
    rate_limit_per_min INTEGER DEFAULT 600,
    is_active INTEGER DEFAULT 1,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS combos (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    strategy TEXT NOT NULL DEFAULT 'priority',
    sticky_limit INTEGER DEFAULT 1,
    timeout_ms INTEGER DEFAULT 30000,
    is_smart INTEGER DEFAULT 0,
    smart_goal TEXT,
    is_active INTEGER DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS combo_steps (
    id TEXT PRIMARY KEY,
    combo_id TEXT NOT NULL REFERENCES combos(id),
    connection_id TEXT NOT NULL REFERENCES connections(id),
    model_id TEXT NOT NULL,
    priority INTEGER NOT NULL,
    weight INTEGER DEFAULT 100,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS request_logs (
    id TEXT PRIMARY KEY,
    timestamp INTEGER NOT NULL,
    connection_id TEXT,
    provider_type_id TEXT,
    model_id TEXT,
    combo_id TEXT,
    modality TEXT NOT NULL,
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    reasoning_tokens INTEGER DEFAULT 0,
    latency_ms INTEGER,
    status_code INTEGER,
    error_message TEXT,
    cost_usd REAL DEFAULT 0,
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_request_logs_timestamp ON request_logs(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_request_logs_provider ON request_logs(provider_type_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_request_logs_connection ON request_logs(connection_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_request_logs_model ON request_logs(model_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_connections_provider ON connections(provider_type_id, status);
CREATE INDEX IF NOT EXISTS idx_connections_status ON connections(status);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS rotation_state (
    combo_id TEXT PRIMARY KEY REFERENCES combos(id),
    counter INTEGER DEFAULT 0,
    updated_at INTEGER NOT NULL
);
`)
	if err != nil {
		return err
	}

	// Incremental migrations for columns added after initial schema.
	// SQLite ALTER TABLE ADD COLUMN is idempotent if column already exists,
	// but SQLite errors on duplicate — just ignore the "duplicate column" error.
	for _, stmt := range []string{
		`ALTER TABLE connections ADD COLUMN provider_specific_data TEXT`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			// Ignore "duplicate column name" errors
			if !isDuplicateColumnError(err) {
				return err
			}
		}
	}

	// Fix provider_types defaults (idempotent upserts)
	now := time.Now().Unix()
	providers := []struct {
		ID, DisplayName, Format, BaseURL string
	}{
		{"ag", "Antigravity", "antigravity", "https://cloudcode-pa.googleapis.com/v1internal:streamProcessMessage"},
		{"cx", "OpenAI Codex", "openai-responses", "https://chatgpt.com/backend-api/codex/responses"},
		{"kiro", "Kiro AI", "openai", "https://api.kiro.ai/v1"},
		{"openai", "OpenAI Platform", "openai", "https://api.openai.com/v1"},
		{"claude", "Anthropic Claude", "anthropic", "https://api.anthropic.com/v1"},
		{"groq", "Groq Cloud", "openai", "https://api.groq.com/openai/v1"},
		{"opencode", "OpenCode Free", "openai", "https://opencode.ai/zen/v1"},
		{"mimocode-free", "MiMoCode Free Tier", "openai", "https://api.xiaomimimo.com/api/free-ai"},
	}
	for _, p := range providers {
		db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, is_custom, created_at) VALUES (?, ?, ?, ?, 0, ?)`,
			p.ID, p.DisplayName, p.Format, p.BaseURL, now)
	}

	// Quota cache table (stores upstream quota data from background scheduler)
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS quota_cache (
	id TEXT PRIMARY KEY,
	connection_id TEXT NOT NULL,
	provider_type_id TEXT NOT NULL,
	connection_name TEXT NOT NULL,
	plan TEXT NOT NULL DEFAULT '',
	quotas TEXT NOT NULL DEFAULT '[]',
	status TEXT NOT NULL DEFAULT 'unknown',
	error TEXT NOT NULL DEFAULT '',
	fetched_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_quota_cache_provider ON quota_cache(provider_type_id);
CREATE INDEX IF NOT EXISTS idx_quota_cache_status ON quota_cache(status);
CREATE INDEX IF NOT EXISTS idx_quota_cache_connection ON quota_cache(connection_id);
	`); err != nil {
		return err
	}

	// Proxy pool tables
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS proxy_pools (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	type TEXT NOT NULL DEFAULT 'http',
	proxy_url TEXT NOT NULL DEFAULT '',
	no_proxy TEXT NOT NULL DEFAULT '',
	relay_auth TEXT NOT NULL DEFAULT '',
	is_active INTEGER DEFAULT 1,
	test_status TEXT NOT NULL DEFAULT 'untested',
	last_tested_at TEXT,
	last_error TEXT,
	response_time_ms INTEGER,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS proxy_groups (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	mode TEXT NOT NULL DEFAULT 'roundrobin',
	sticky_limit INTEGER DEFAULT 1,
	strict_proxy INTEGER DEFAULT 0,
	proxy_pool_ids TEXT NOT NULL DEFAULT '[]',
	is_active INTEGER DEFAULT 1,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
	`); err != nil {
		return err
	}

	return nil
}

func isDuplicateColumnError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "duplicate column") || contains(msg, "already exists")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
