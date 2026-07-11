package db

import (
	"database/sql"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/errorcode"
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
		`ALTER TABLE request_logs ADD COLUMN cached_tokens INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			// Ignore "duplicate column name" errors
			if !isDuplicateColumnError(err) {
				return err
			}
		}
	}

	// Backfill status_code for legacy request_logs rows that only recorded the
	// error message. This is idempotent: rows with a non-zero status_code keep it.
	if err := migrateRequestLogStatusCodes(db); err != nil {
		return err
	}

	// Fix provider_types defaults (idempotent upserts)
	now := time.Now().Unix()
	providers := []struct {
		ID, DisplayName, Format, BaseURL string
	}{
		{"ag", "Antigravity", "antigravity", "https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse"},
		{"cx", "OpenAI Codex", "openai-responses", "https://chatgpt.com/backend-api/codex/responses"},
		{"kiro", "Kiro AI", "openai", "https://api.kiro.ai/v1"},
		{"openai", "OpenAI Platform", "openai", "https://api.openai.com/v1"},
		{"claude", "Anthropic Claude", "anthropic", "https://api.anthropic.com/v1"},
		{"gemini", "Gemini", "gemini", "https://generativelanguage.googleapis.com/v1beta"},
		{"deepseek", "DeepSeek", "openai", "https://api.deepseek.com/v1"},
		{"groq", "Groq Cloud", "openai", "https://api.groq.com/openai/v1"},
		{"openrouter", "OpenRouter", "openai", "https://openrouter.ai/api/v1"},
		{"oc", "OpenCode Free", "openai", "https://opencode.ai/zen/v1"},
		{"oc-zen", "OpenCode Zen", "openai", "https://opencode.ai/zen/v1"},
		{"oc-go", "OpenCode Go", "openai", "https://opencode.ai/zen/go/v1"},
		{"mimocode", "MiMoCode", "openai", "https://api.xiaomimimo.com/api/free-ai/openai"},
		{"mimocode-free", "MiMoCode Free Tier", "openai", "https://api.xiaomimimo.com/api/free-ai/openai"},
		{"cf", "Cloudflare Workers AI", "openai", "https://api.cloudflare.com/client/v4/accounts/{accountId}/ai/v1/chat/completions"},
	}
	for _, p := range providers {
		db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, is_custom, created_at) VALUES (?, ?, ?, ?, 0, ?)`,
			p.ID, p.DisplayName, p.Format, p.BaseURL, now)
	}

	// Normalize legacy `opencode` provider type to canonical `oc` alias, keeping
	// connections and quota cache consistent. Must run after seeding `oc` above.
	db.Exec(`UPDATE connections SET provider_type_id = 'oc' WHERE provider_type_id = 'opencode'`)
	db.Exec(`UPDATE quota_cache SET provider_type_id = 'oc' WHERE provider_type_id = 'opencode'`)
	db.Exec(`DELETE FROM provider_types WHERE id = 'opencode'`)
// Seed a default direct connection for OpenCode Free (oc). This connection
// is always-on, cannot be deleted, and serves as the direct route. Additional
// oc connections must use a proxy pool (provider_specific_data.proxyPoolId).
var ocDirectCount int
db.QueryRow(`SELECT COUNT(*) FROM connections WHERE provider_type_id = 'oc' AND is_active = 1 AND provider_specific_data LIKE '%"direct":"true"%'`).Scan(&ocDirectCount)
if ocDirectCount == 0 {
	db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, provider_specific_data, status, is_active, created_at, updated_at) VALUES ('oc-direct-default', 'oc', 'Direct (Default)', 'none', '{"direct":"true"}', 'ready', 1, ?, ?)`, now, now)
}
// Deactivate stale oc connections that have incorrect auth_type (should be 'none').
db.Exec(`UPDATE connections SET is_active = 0 WHERE provider_type_id = 'oc' AND auth_type != 'none'`)

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

	// Connection priority and auto-disable columns
	for _, stmt := range []string{
		`ALTER TABLE connections ADD COLUMN priority INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE connections ADD COLUMN consecutive_ban_count INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			if !isDuplicateColumnError(err) {
				return err
			}
		}
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

	// Proxy pool IP info columns
	for _, stmt := range []string{
		`ALTER TABLE proxy_pools ADD COLUMN proxy_ip TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE proxy_pools ADD COLUMN proxy_country TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE proxy_pools ADD COLUMN proxy_city TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE proxy_pools ADD COLUMN proxy_org TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			if !isDuplicateColumnError(err) {
				return err
			}
		}
	}

	// Response cache table (Phase 1: schema ready for Phase 2 persistence)
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS response_cache (
	hash TEXT PRIMARY KEY,
	body TEXT NOT NULL,
	status_code INTEGER NOT NULL,
	content_type TEXT NOT NULL DEFAULT 'application/json',
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_response_cache_expires ON response_cache(expires_at);
	`); err != nil {
		return err
	}

	// Model pricing — single source of truth for per-model cost rates.
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS model_pricing (
	model_id TEXT PRIMARY KEY,
	display_name TEXT NOT NULL DEFAULT '',
	input_per_1k REAL NOT NULL DEFAULT 0,
	output_per_1k REAL NOT NULL DEFAULT 0,
	reason_per_1k REAL NOT NULL DEFAULT 0,
	cached_read_per_1k REAL NOT NULL DEFAULT 0,
	cached_write_per_1k REAL NOT NULL DEFAULT 0,
	image_per_unit REAL NOT NULL DEFAULT 0,
	audio_per_min REAL NOT NULL DEFAULT 0,
	currency TEXT NOT NULL DEFAULT 'USD',
	updated_at INTEGER NOT NULL DEFAULT 0
);
	`); err != nil {
		return err
	}

	// Seed default model pricing (INSERT OR IGNORE — never overwrites operator edits).
	now = time.Now().Unix()
	seedPricing := []struct {
		ID, Name                                 string
		In, Out, Reason, CachedRead, CachedWrite float64
	}{
		// OpenAI
		{"gpt-4o", "GPT-4o", 0.0025, 0.01, 0, 0.00125, 0},
		{"gpt-4o-mini", "GPT-4o mini", 0.00015, 0.0006, 0, 0.000075, 0},
		{"gpt-4-turbo", "GPT-4 Turbo", 0.01, 0.03, 0, 0, 0},
		{"gpt-4", "GPT-4", 0.03, 0.06, 0, 0, 0},
		{"gpt-3.5-turbo", "GPT-3.5 Turbo", 0.0005, 0.0015, 0, 0, 0},
		{"o1", "OpenAI o1", 0.015, 0.06, 0.06, 0.0075, 0},
		{"o1-mini", "OpenAI o1-mini", 0.0011, 0.0044, 0.0044, 0.00055, 0},
		{"o3", "OpenAI o3", 0.002, 0.008, 0.008, 0.0005, 0},
		{"o3-mini", "OpenAI o3-mini", 0.0011, 0.0044, 0.0044, 0.00055, 0},
		{"o4-mini", "OpenAI o4-mini", 0.0011, 0.0044, 0.0044, 0.000275, 0},
		{"gpt-5", "GPT-5", 0.00125, 0.01, 0, 0.000125, 0},
		{"gpt-5-mini", "GPT-5 mini", 0.00025, 0.002, 0, 0.000025, 0},
		{"gpt-5-nano", "GPT-5 nano", 0.00005, 0.0004, 0, 0.000005, 0},
		{"gpt-5-pro", "GPT-5 Pro", 0.015, 0.12, 0, 0, 0},
		{"gpt-4.1", "GPT-4.1", 0.002, 0.008, 0, 0.0005, 0},
		{"gpt-4.1-mini", "GPT-4.1 mini", 0.0004, 0.0016, 0, 0.0001, 0},
		{"gpt-4.1-nano", "GPT-4.1 nano", 0.0001, 0.0004, 0, 0.000025, 0},
		// Anthropic (bare + dated served keys)
		{"claude-3-5-haiku", "Claude 3.5 Haiku", 0.0008, 0.004, 0, 0.00008, 0.001},
		{"claude-3-5-haiku-20241022", "Claude 3.5 Haiku", 0.0008, 0.004, 0, 0.00008, 0.001},
		{"claude-3-5-sonnet", "Claude 3.5 Sonnet", 0.003, 0.015, 0, 0.0003, 0.00375},
		{"claude-3-7-sonnet", "Claude 3.7 Sonnet", 0.003, 0.015, 0, 0.0003, 0.00375},
		{"claude-opus-4", "Claude Opus 4", 0.015, 0.075, 0, 0.0015, 0.01875},
		{"claude-opus-4-20250514", "Claude Opus 4", 0.015, 0.075, 0, 0.0015, 0.01875},
		{"claude-sonnet-4", "Claude Sonnet 4", 0.003, 0.015, 0, 0.0003, 0.00375},
		{"claude-sonnet-4-20250514", "Claude Sonnet 4", 0.003, 0.015, 0, 0.0003, 0.00375},
		{"claude-opus-4-1", "Claude Opus 4.1", 0.015, 0.075, 0, 0.0015, 0.01875},
		{"claude-opus-4-1-20250805", "Claude Opus 4.1", 0.015, 0.075, 0, 0.0015, 0.01875},
		{"claude-opus-4-5", "Claude Opus 4.5", 0.005, 0.025, 0, 0.0005, 0.00625},
		{"claude-opus-4-5-20251101", "Claude Opus 4.5", 0.005, 0.025, 0, 0.0005, 0.00625},
		{"claude-sonnet-4-5", "Claude Sonnet 4.5", 0.003, 0.015, 0, 0.0003, 0.00375},
		{"claude-sonnet-4-5-20250929", "Claude Sonnet 4.5", 0.003, 0.015, 0, 0.0003, 0.00375},
		{"claude-opus-4-6", "Claude Opus 4.6", 0.005, 0.025, 0, 0.0005, 0.00625},
		{"claude-haiku-4-5", "Claude Haiku 4.5", 0.001, 0.005, 0, 0.0001, 0.00125},
		{"claude-haiku-4-5-20251001", "Claude Haiku 4.5", 0.001, 0.005, 0, 0.0001, 0.00125},
		// Google
		{"gemini-2.5-pro", "Gemini 2.5 Pro", 0.00125, 0.01, 0, 0.000125, 0.000125},
		{"gemini-2.5-flash", "Gemini 2.5 Flash", 0.0003, 0.0025, 0, 0.00003, 0.00003},
		{"gemini-2.5-flash-lite", "Gemini 2.5 Flash Lite", 0.0001, 0.0004, 0, 0.00001, 0.00001},
		{"gemini-2.0-flash", "Gemini 2.0 Flash", 0.0001, 0.0004, 0, 0.000025, 0.000025},
		{"gemini-2.0-flash-lite", "Gemini 2.0 Flash Lite", 0.000075, 0.0003, 0, 0, 0},
		// DeepSeek (USD native)
		{"deepseek/deepseek-chat", "DeepSeek Chat", 0.00014, 0.00028, 0, 0.0000028, 0},
		{"deepseek/deepseek-reasoner", "DeepSeek Reasoner", 0.00014, 0.00028, 0.00028, 0.0000028, 0},
		// Groq
		{"llama-3.3-70b-versatile", "Llama 3.3 70B", 0.00059, 0.00079, 0, 0, 0},
		{"llama-3.1-8b-instant", "Llama 3.1 8B", 0.00005, 0.00008, 0, 0, 0},
		{"meta-llama/llama-4-scout-17b-16e-instruct", "Llama 4 Scout", 0.00011, 0.00034, 0, 0, 0},
		// Moonshot Kimi (CNY→USD @7.15)
		{"kimi-k2.5", "Kimi K2.5", 0.000559, 0.002937, 0, 0.0000979, 0},
		{"kimi-k2.6", "Kimi K2.6", 0.000909, 0.003776, 0, 0.0001538, 0},
		{"kimi-k2.7-code", "Kimi K2.7 Code", 0.000909, 0.003776, 0, 0.0001818, 0},
		{"kimi-k2.7-code-highspeed", "Kimi K2.7 Code HighSpeed", 0.001818, 0.007552, 0, 0.0003636, 0},
		{"kimi-k2", "Kimi K2", 0.0006, 0.0024, 0, 0, 0},
		// MiMo v2 family
		{"mimo-v2-pro", "MiMo v2 Pro", 0.001, 0.002, 0, 0, 0},
		{"mimo-v2", "MiMo v2", 0.0005, 0.001, 0, 0, 0},
		{"mimo-v2-flash", "MiMo v2 Flash", 0.0001, 0.0002, 0, 0, 0},
		{"mimo-v2-omni", "MiMo v2 Omni", 0.001, 0.002, 0, 0, 0},
		// Relay / free providers — notional $0
		{"codex-free/deepseek-v4-flash-free", "Codex Free", 0, 0, 0, 0, 0},
		{"codex-free/mimo-v2.5-free", "Codex Free", 0, 0, 0, 0, 0},
		{"codex-free/hy3-free", "Codex Free", 0, 0, 0, 0, 0},
		{"codex-free/nemotron-3-ultra-free", "Codex Free", 0, 0, 0, 0, 0},
		{"codex-free/north-mini-code-free", "Codex Free", 0, 0, 0, 0, 0},
		{"oc/deepseek-v4-flash-free", "OC Free", 0, 0, 0, 0, 0},
		{"oc/mimo-v2.5-free", "OC Free", 0, 0, 0, 0, 0},
		{"oc/hy3-free", "OC Free", 0, 0, 0, 0, 0},
		{"oc/nemotron-3-ultra-free", "OC Free", 0, 0, 0, 0, 0},
		{"oc/north-mini-code-free", "OC Free", 0, 0, 0, 0, 0},
		{"mimocode/mimo-auto", "MiMo", 0, 0, 0, 0, 0},
		{"mimocode-free/mimo-auto", "MiMo Free", 0, 0, 0, 0, 0},
		{"mimo/mimo-auto", "MiMo", 0, 0, 0, 0, 0},
		{"mimo-tp/mimo-auto", "MiMo TP", 0, 0, 0, 0, 0},
		{"mimo-token/mimo-auto", "MiMo Token", 0, 0, 0, 0, 0},
		{"oc-go/glm-5", "GLM 5", 0, 0, 0, 0, 0},
		{"oc-go/glm-5.1", "GLM 5.1", 0, 0, 0, 0, 0},
		{"oc-go/glm-5.2", "GLM 5.2", 0, 0, 0, 0, 0},
		{"oc-go/hy3-preview", "HY3 Preview", 0, 0, 0, 0, 0},
		{"oc-go/minimax-m2.5", "MiniMax M2.5", 0, 0, 0, 0, 0},
		{"oc-go/minimax-m2.7", "MiniMax M2.7", 0, 0, 0, 0, 0},
		{"oc-go/minimax-m3", "MiniMax M3", 0, 0, 0, 0, 0},
		{"oc-go/qwen3.5-plus", "Qwen 3.5 Plus", 0, 0, 0, 0, 0},
		{"oc-go/qwen3.6-plus", "Qwen 3.6 Plus", 0, 0, 0, 0, 0},
		{"oc-go/qwen3.7-max", "Qwen 3.7 Max", 0, 0, 0, 0, 0},
		{"oc-go/qwen3.7-plus", "Qwen 3.7 Plus", 0, 0, 0, 0, 0},
		{"oc-zen/big-pickle", "Big Pickle", 0, 0, 0, 0, 0},
		{"oc-zen/hy3-preview", "HY3 Preview", 0, 0, 0, 0, 0},
		{"oc-zen/glm-5", "GLM 5", 0, 0, 0, 0, 0},
		{"oc-zen/glm-5.1", "GLM 5.1", 0, 0, 0, 0, 0},
		{"oc-zen/glm-5.2", "GLM 5.2", 0, 0, 0, 0, 0},
		{"oc-zen/grok-build-0.1", "Grok Build", 0, 0, 0, 0, 0},
		{"oc-zen/minimax-m2.5", "MiniMax M2.5", 0, 0, 0, 0, 0},
		{"oc-zen/minimax-m2.7", "MiniMax M2.7", 0, 0, 0, 0, 0},
		{"oc-zen/minimax-m3", "MiniMax M3", 0, 0, 0, 0, 0},
		{"oc-zen/mimo-v2.5-free", "OC Zen Free", 0, 0, 0, 0, 0},
		{"oc-zen/nemotron-3-ultra-free", "OC Zen Free", 0, 0, 0, 0, 0},
		{"oc-zen/north-mini-code-free", "OC Zen Free", 0, 0, 0, 0, 0},
		{"oc-zen/qwen3.5-plus", "Qwen 3.5 Plus", 0, 0, 0, 0, 0},
		{"oc-zen/qwen3.6-plus", "Qwen 3.6 Plus", 0, 0, 0, 0, 0},
	}
	for _, p := range seedPricing {
		if _, err := db.Exec(`INSERT OR IGNORE INTO model_pricing
			(model_id, display_name, input_per_1k, output_per_1k, reason_per_1k, cached_read_per_1k, cached_write_per_1k, currency, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'USD', ?)`,
			p.ID, p.Name, p.In, p.Out, p.Reason, p.CachedRead, p.CachedWrite, now); err != nil {
			return err
		}
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

// migrateRequestLogStatusCodes fills status_code for rows that only captured an
// error message such as "stream error 429: ...". It keeps the DB consistent
// with the runtime tracker, which now derives the status code automatically.
func migrateRequestLogStatusCodes(database *sql.DB) error {
	rows, err := database.Query(`SELECT id, error_message FROM request_logs WHERE (status_code = 0 OR status_code IS NULL) AND error_message IS NOT NULL AND error_message != ''`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type row struct {
		id   string
		text string
	}
	var toUpdate []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.text); err != nil {
			return err
		}
		if code := errorcode.FromString(r.text); code != 0 {
			toUpdate = append(toUpdate, r)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(toUpdate) == 0 {
		return nil
	}

	stmt, err := database.Prepare(`UPDATE request_logs SET status_code = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, r := range toUpdate {
		code := errorcode.FromString(r.text)
		if _, err := stmt.Exec(code, r.id); err != nil {
			return err
		}
	}
	return nil
}
