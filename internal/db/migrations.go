package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
		category TEXT DEFAULT 'apikey',
		service_kinds TEXT DEFAULT '["llm"]',
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
  stream INTEGER NOT NULL DEFAULT 0,
  latency_ms INTEGER,
  status_code INTEGER,
  error_message TEXT,
  cost_usd REAL DEFAULT 0,
  client_ip TEXT,
  user_agent TEXT,
  created_at INTEGER NOT NULL
);

	CREATE INDEX IF NOT EXISTS idx_request_logs_timestamp ON request_logs(timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_request_logs_provider ON request_logs(provider_type_id, timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_request_logs_connection ON request_logs(connection_id, timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_request_logs_model ON request_logs(model_id, timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_request_logs_usage ON request_logs(provider_type_id, model_id, timestamp DESC, status_code);
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
		`ALTER TABLE request_logs ADD COLUMN cache_creation_tokens INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE request_logs ADD COLUMN stream INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE api_keys ADD COLUMN key_value TEXT`,
		`ALTER TABLE request_logs ADD COLUMN api_key_id TEXT`,
		`ALTER TABLE api_keys ADD COLUMN max_tokens INTEGER DEFAULT 0`,
		`ALTER TABLE api_keys ADD COLUMN expires_at INTEGER`,
		`ALTER TABLE request_logs ADD COLUMN tokens_estimated INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE request_logs ADD COLUMN proxy_pool_id TEXT`,
    `ALTER TABLE request_logs ADD COLUMN api_type TEXT`,
    `ALTER TABLE request_logs ADD COLUMN client_ip TEXT`,
    `ALTER TABLE request_logs ADD COLUMN user_agent TEXT`,
    `CREATE INDEX IF NOT EXISTS idx_request_logs_api_key ON request_logs(api_key_id, timestamp DESC)`,
		`ALTER TABLE provider_types ADD COLUMN category TEXT DEFAULT 'apikey'`,
		`ALTER TABLE provider_types ADD COLUMN skip_key_validation INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE provider_types ADD COLUMN service_kinds TEXT DEFAULT '["llm"]'`,
		`ALTER TABLE combos ADD COLUMN fusion_config TEXT`,
		`CREATE TABLE IF NOT EXISTS compression_metrics (
    mode TEXT PRIMARY KEY,
    requests INTEGER NOT NULL DEFAULT 0,
    original_tokens INTEGER NOT NULL DEFAULT 0,
    compressed_tokens INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL DEFAULT 0
)`,
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

	// Lifetime token budget tracking per proxy API key.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS api_key_usage (
			api_key_id TEXT PRIMARY KEY REFERENCES api_keys(id),
			total_tokens INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL
		)
	`); err != nil {
		return err
	}

	if err := migrateRequestLogStatusCodes(db); err != nil {
		return err
	}

	// Fix provider_types defaults (idempotent upserts)
	now := time.Now().Unix()
	providers := []struct {
		ID, DisplayName, Format, BaseURL, Category string
		ServiceKinds                               []string
	}{
		{"ag", "Antigravity", "antigravity", "https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse", "oauth", []string{"llm"}},
		{"cx", "OpenAI Codex", "openai-responses", "https://chatgpt.com/backend-api/codex/responses", "oauth", []string{"llm"}},
		{"kiro", "Kiro AI", "openai", "https://api.kiro.ai/v1", "oauth", []string{"llm"}},
		{"openai", "OpenAI Platform", "openai", "https://api.openai.com/v1", "apikey", []string{"llm", "embedding", "image"}},
		{"claude", "Anthropic Claude", "anthropic", "https://api.anthropic.com/v1", "apikey", []string{"llm"}},
		{"gemini", "Gemini", "gemini", "https://generativelanguage.googleapis.com/v1beta", "apikey", []string{"llm"}},
		{"deepseek", "DeepSeek", "openai", "https://api.deepseek.com/v1", "apikey", []string{"llm"}},
		{"groq", "Groq Cloud", "openai", "https://api.groq.com/openai/v1", "apikey", []string{"llm"}},
		{"openrouter", "OpenRouter", "openai", "https://openrouter.ai/api/v1", "apikey", []string{"llm"}},
		{"oc", "OpenCode Free", "openai", "https://opencode.ai/zen/v1", "no-auth", []string{"llm"}},
		{"oc-zen", "OpenCode Zen", "openai", "https://opencode.ai/zen/v1", "apikey", []string{"llm"}},
		{"oc-go", "OpenCode Go", "openai", "https://opencode.ai/zen/go/v1", "apikey", []string{"llm"}},
		{"mimocode", "MiMoCode Free", "openai", "https://api.xiaomimimo.com/api/free-ai/openai", "no-auth", []string{"llm"}},
		{"mimo", "Xiaomi MiMo PAYG", "openai", "https://api.xiaomimimo.com/v1", "apikey", []string{"llm"}},
		{"mimo-tp", "MiMo Token Plan", "openai", "https://api.xiaomimimo.com/v1", "apikey", []string{"llm"}},
		{"cf", "Cloudflare Workers AI", "openai", "https://api.cloudflare.com/client/v4/accounts/{accountId}/ai/v1/chat/completions", "apikey", []string{"llm", "embedding", "image"}},
		{"glm", "Zhipu GLM", "openai", "https://api.z.ai/api/paas/v4", "apikey", []string{"llm"}},
		{"minimax", "MiniMax", "openai", "https://api.minimax.io/v1", "apikey", []string{"llm"}},
		{"kimi", "Kimi", "openai", "https://api.moonshot.ai/v1", "apikey", []string{"llm"}},
		{"mistral", "Mistral AI", "openai", "https://api.mistral.ai/v1", "apikey", []string{"llm"}},
		{"cerebras", "Cerebras", "openai", "https://api.cerebras.ai/v1", "apikey", []string{"llm"}},
		{"together", "Together AI", "openai", "https://api.together.ai/v1", "apikey", []string{"llm"}},
		{"fireworks", "Fireworks", "openai", "https://api.fireworks.ai/inference/v1", "apikey", []string{"llm"}},
		{"novita", "Novita AI", "openai", "https://api.novita.ai/openai/v1", "apikey", []string{"llm"}},
		{"lambda", "Lambda", "openai", "https://api.lambda.ai/v1", "apikey", []string{"llm"}},
		{"pollinations", "Pollinations.AI", "openai", "https://gen.pollinations.ai/v1", "apikey", []string{"llm"}},
		{"zenmux", "ZenMux", "openai", "https://zenmux.ai/api/v1", "apikey", []string{"llm"}},

	{"copilot", "GitHub Copilot", "openai", "https://api.githubcopilot.com", "oauth", []string{"llm"}},

	{"grok-cli", "Grok CLI (Grok Build)", "grok-cli", "https://cli-chat-proxy.grok.com/v1/responses", "oauth", []string{"llm"}},

	{"devin", "Devin CLI", "devin-cli", "", "apikey", []string{"llm"}},
	{"qoder", "Qoder", "qoder", "https://dashscope.aliyuncs.com/compatible-mode/v1", "apikey", []string{"llm"}},

	{"codebuddy", "CodeBuddy", "openai", "https://copilot.tencent.com/v2/chat/completions", "oauth", []string{"llm"}},

	{"vertex", "Google Vertex AI", "openai", "https://aiplatform.googleapis.com/v1/projects/{projectId}/locations/{location}/endpoints/openapi", "service-account", []string{"llm"}},
		{"bedrock", "Amazon Bedrock Mantle", "openai", "https://bedrock-mantle.{region}.api.aws/v1", "apikey", []string{"llm"}},
	}
	for _, p := range providers {
		serviceKindsJSON, _ := json.Marshal(p.ServiceKinds)
		db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, is_custom, category, service_kinds, created_at) VALUES (?, ?, ?, ?, 0, ?, ?, ?)`,
			p.ID, p.DisplayName, p.Format, p.BaseURL, p.Category, string(serviceKindsJSON), now)
		db.Exec(`UPDATE provider_types SET category = ?, service_kinds = ? WHERE id = ?`,
			p.Category, string(serviceKindsJSON), p.ID)
	}

	// Repair legacy Grok CLI base_url rows that point at /v1 instead of /v1/responses.
	// The executor now hardens this too, but fixing the DB avoids confusion in the UI.
	if _, err := db.Exec(`UPDATE provider_types SET base_url = 'https://cli-chat-proxy.grok.com/v1/responses' WHERE id = 'grok-cli' AND base_url IN ('https://cli-chat-proxy.grok.com/v1', 'https://cli-chat-proxy.grok.com/v1/', 'https://cli-chat-proxy.grok.com/', 'https://cli-chat-proxy.grok.com')`); err != nil {
		return err
	}

	// Normalize legacy `opencode` provider type to canonical `oc` alias, keeping
	// connections and quota cache consistent. Must run after seeding `oc` above.
	db.Exec(`UPDATE connections SET provider_type_id = 'oc' WHERE provider_type_id = 'opencode'`)
	db.Exec(`UPDATE quota_cache SET provider_type_id = 'oc' WHERE provider_type_id = 'opencode'`)
	db.Exec(`DELETE FROM provider_types WHERE id = 'opencode'`)

	// Normalize legacy `mimocode-free` provider type to canonical `mimocode` alias.
	db.Exec(`UPDATE connections SET provider_type_id = 'mimocode' WHERE provider_type_id = 'mimocode-free'`)
	db.Exec(`UPDATE quota_cache SET provider_type_id = 'mimocode' WHERE provider_type_id = 'mimocode-free'`)
	db.Exec(`INSERT OR IGNORE INTO provider_models (provider_type_id, model, created_at) SELECT 'mimocode', model, created_at FROM provider_models WHERE provider_type_id = 'mimocode-free'`)
	db.Exec(`DELETE FROM provider_models WHERE provider_type_id = 'mimocode-free'`)
	db.Exec(`DELETE FROM provider_types WHERE id = 'mimocode-free'`)

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

	// Seed a default direct connection for MiMoCode. This connection is always-on,
	// cannot be deleted, and serves as the direct route. Additional mimocode
	// connections must use a proxy pool (provider_specific_data.proxyPoolId).
	var mimocodeDirectCount int
	db.QueryRow(`SELECT COUNT(*) FROM connections WHERE provider_type_id = 'mimocode' AND is_active = 1 AND provider_specific_data LIKE '%"direct":"true"%'`).Scan(&mimocodeDirectCount)
	if mimocodeDirectCount == 0 {
		mimocodePSD, _ := json.Marshal(map[string]string{
			"direct":       "true",
			"accountId":    "mimocode-default",
			"accountLabel": "Default",
			"fingerprint":  generateFingerprint(),
		})
		db.Exec(`INSERT OR IGNORE INTO connections (id, provider_type_id, name, auth_type, provider_specific_data, status, is_active, created_at, updated_at) VALUES ('mimocode-direct-default', 'mimocode', 'Direct (Default)', 'none', ?, 'ready', 1, ?, ?)`, string(mimocodePSD), now, now)
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

	// User-added custom models for custom providers.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS provider_models (
		provider_type_id TEXT NOT NULL,
		model TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		PRIMARY KEY (provider_type_id, model)
);`); err != nil {
		return err
	}

	// Seed default model pricing (INSERT OR IGNORE — never overwrites operator edits).
	now = time.Now().Unix()
	seedPricing := []struct {
		ID, Name                                 string
		In, Out, Reason, CachedRead, CachedWrite float64
	}{
		// ── OpenAI (per 1K tokens) ──
		{"gpt-4o", "GPT-4o", 0.0025, 0.01, 0, 0.00125, 0},
		{"gpt-4o-mini", "GPT-4o mini", 0.00015, 0.0006, 0, 0.000075, 0},
		{"gpt-4-turbo", "GPT-4 Turbo", 0.01, 0.03, 0, 0, 0},
		{"gpt-4", "GPT-4", 0.03, 0.06, 0, 0, 0},
		{"gpt-3.5-turbo", "GPT-3.5 Turbo", 0.0005, 0.0015, 0, 0, 0},
		{"o1", "OpenAI o1", 0.015, 0.06, 0.06, 0.0075, 0},
		{"o1-mini", "OpenAI o1-mini", 0.0011, 0.0044, 0.0044, 0.00055, 0},
		{"o1-pro", "OpenAI o1-pro", 0.15, 0.6, 0.6, 0, 0},
		{"o3", "OpenAI o3", 0.002, 0.008, 0.008, 0.0005, 0},
		{"o3-mini", "OpenAI o3-mini", 0.0011, 0.0044, 0.0044, 0.00055, 0},
		{"o4-mini", "OpenAI o4-mini", 0.0011, 0.0044, 0.0044, 0.000275, 0},
		{"gpt-5", "GPT-5", 0.00125, 0.01, 0, 0.000125, 0},
		{"gpt-5-mini", "GPT-5 mini", 0.00025, 0.002, 0, 0.000025, 0},
		{"gpt-5-nano", "GPT-5 nano", 0.00005, 0.0004, 0, 0.000005, 0},
		{"gpt-5-pro", "GPT-5 Pro", 0.015, 0.12, 0, 0, 0},
		{"gpt-5-codex", "GPT-5 Codex", 0.0025, 0.01, 0, 0, 0},
		{"gpt-5.1", "GPT-5.1", 0.0015, 0.012, 0, 0.00015, 0},
		{"gpt-5.1-codex", "GPT-5.1 Codex", 0.0025, 0.01, 0, 0, 0},
		{"gpt-5.1-codex-max", "GPT-5.1 Codex Max", 0.005, 0.02, 0, 0, 0},
		{"gpt-5.1-codex-mini", "GPT-5.1 Codex Mini", 0.001, 0.004, 0, 0, 0},
		{"gpt-5.2", "GPT-5.2", 0.002, 0.015, 0, 0.0002, 0},
		{"gpt-5.2-codex", "GPT-5.2 Codex", 0.003, 0.012, 0, 0, 0},
		{"gpt-5.3-codex", "GPT-5.3 Codex", 0.003, 0.012, 0, 0, 0},
		{"gpt-5.3-codex-spark", "GPT-5.3 Codex Spark", 0.001, 0.004, 0, 0, 0},
		{"gpt-5.4", "GPT-5.4", 0.0025, 0.02, 0, 0.00025, 0},
		{"gpt-5.4-mini", "GPT-5.4 mini", 0.0004, 0.003, 0, 0.00004, 0},
		{"gpt-5.4-nano", "GPT-5.4 nano", 0.00008, 0.0006, 0, 0.000008, 0},
		{"gpt-5.4-pro", "GPT-5.4 Pro", 0.02, 0.15, 0, 0, 0},
		{"gpt-5.5", "GPT-5.5", 0.003, 0.025, 0, 0.0003, 0},
		{"gpt-5.5-pro", "GPT-5.5 Pro", 0.025, 0.2, 0, 0, 0},
		{"gpt-4.1", "GPT-4.1", 0.002, 0.008, 0, 0.0005, 0},
		{"gpt-4.1-mini", "GPT-4.1 mini", 0.0004, 0.0016, 0, 0.0001, 0},
		{"gpt-4.1-nano", "GPT-4.1 nano", 0.0001, 0.0004, 0, 0.000025, 0},
		{"codex-mini", "Codex Mini", 0.0015, 0.006, 0, 0, 0},

		// ── Anthropic ──
		{"claude-3-opus", "Claude 3 Opus", 0.015, 0.075, 0, 0, 0},
		{"claude-3.5-haiku", "Claude 3.5 Haiku", 0.0008, 0.004, 0, 0.00008, 0.001},
		{"claude-3.5-sonnet", "Claude 3.5 Sonnet", 0.003, 0.015, 0, 0.0003, 0.00375},
		{"claude-3.7-sonnet", "Claude 3.7 Sonnet", 0.003, 0.015, 0, 0.0003, 0.00375},
		{"claude-sonnet-4", "Claude Sonnet 4", 0.003, 0.015, 0, 0.0003, 0.00375},
		{"claude-sonnet-4-5", "Claude Sonnet 4.5", 0.003, 0.015, 0, 0.0003, 0.00375},
		{"claude-sonnet-4-6", "Claude Sonnet 4.6", 0.003, 0.015, 0, 0.0003, 0.00375},
		{"claude-opus-4", "Claude Opus 4", 0.015, 0.075, 0, 0.0015, 0.01875},
		{"claude-opus-4-1", "Claude Opus 4.1", 0.015, 0.075, 0, 0.0015, 0.01875},
		{"claude-opus-4-5", "Claude Opus 4.5", 0.005, 0.025, 0, 0.0005, 0.00625},
		{"claude-opus-4-6", "Claude Opus 4.6", 0.005, 0.025, 0, 0.0005, 0.00625},
		{"claude-haiku-4-5", "Claude Haiku 4.5", 0.001, 0.005, 0, 0.0001, 0.00125},
		{"claude-opus-4-7", "Claude Opus 4.7", 0.005, 0.025, 0, 0.0005, 0.00625},
		{"claude-opus-4-8", "Claude Opus 4.8", 0.005, 0.025, 0, 0.0005, 0.00625},
		{"claude-fable-5", "Claude Fable 5", 0.003, 0.015, 0, 0.0003, 0.00375},
		{"claude-sonnet-5", "Claude Sonnet 5", 0.003, 0.015, 0, 0.0003, 0.00375},

		// ── Google Gemini ──
		{"gemini-2.5-pro", "Gemini 2.5 Pro", 0.00125, 0.01, 0, 0.000125, 0.000125},
		{"gemini-2.5-flash", "Gemini 2.5 Flash", 0.0003, 0.0025, 0, 0.00003, 0.00003},
		{"gemini-2.5-flash-lite", "Gemini 2.5 Flash Lite", 0.0001, 0.0004, 0, 0.00001, 0.00001},
		{"gemini-2.0-flash", "Gemini 2.0 Flash", 0.0001, 0.0004, 0, 0.000025, 0.000025},
		{"gemini-2.0-flash-lite", "Gemini 2.0 Flash Lite", 0.000075, 0.0003, 0, 0, 0},
		{"gemini-3-pro-preview", "Gemini 3 Pro Preview", 0.00125, 0.01, 0, 0.000125, 0},
		{"gemini-3-flash", "Gemini 3 Flash", 0.00015, 0.0006, 0, 0.000015, 0},

		// ── DeepSeek ──
		{"deepseek-chat", "DeepSeek Chat", 0.00014, 0.00028, 0, 0.0000028, 0},
		{"deepseek-coder", "DeepSeek Coder", 0.00014, 0.00028, 0, 0, 0},
		{"deepseek-reasoner", "DeepSeek Reasoner", 0.00014, 0.00028, 0.00028, 0.0000028, 0},
		{"deepseek-r1", "DeepSeek R1", 0.00055, 0.00219, 0, 0.00014, 0},
		{"deepseek-v3", "DeepSeek V3", 0.00027, 0.0011, 0, 0.00007, 0},
		{"deepseek-v4-flash", "DeepSeek v4 Flash", 0.0001, 0.0004, 0, 0, 0},
		{"deepseek-v4-pro", "DeepSeek v4 Pro", 0.0005, 0.002, 0, 0, 0},

		// ── Meta / Llama ──
		{"llama-3.3-70b-versatile", "Llama 3.3 70B", 0.00059, 0.00079, 0, 0, 0},
		{"llama-3.1-8b-instant", "Llama 3.1 8B", 0.00005, 0.00008, 0, 0, 0},
		{"llama-3.1-70b-versatile", "Llama 3.1 70B", 0.00059, 0.00079, 0, 0, 0},
		{"llama-4-scout", "Llama 4 Scout", 0.00011, 0.00034, 0, 0, 0},
		{"llama-4-maverick", "Llama 4 Maverick", 0.0002, 0.0006, 0, 0, 0},

	// ── xAI Grok ──
	{"grok-build", "Grok Build", 0.0003, 0.0005, 0, 0, 0},
	{"grok-4.5", "Grok 4.5", 0.005, 0.025, 0, 0, 0},
	{"grok-4.5-high", "Grok 4.5 (High)", 0.005, 0.025, 0, 0, 0},
	{"grok-4.5-medium", "Grok 4.5 (Medium)", 0.005, 0.025, 0, 0, 0},
	{"grok-4.5-low", "Grok 4.5 (Low)", 0.005, 0.025, 0, 0, 0},

		// ── Moonshot Kimi ──
		{"kimi-k2", "Kimi K2", 0.000559, 0.002378, 0, 0, 0},
		{"kimi-k2.5", "Kimi K2.5", 0.000559, 0.002937, 0, 0.0000979, 0},
		{"kimi-k2.6", "Kimi K2.6", 0.000909, 0.003776, 0, 0.0001538, 0},
		{"kimi-k2.7-code", "Kimi K2.7 Code", 0.000909, 0.003776, 0, 0.0001818, 0},
		{"kimi-k2.7-code-highspeed", "Kimi K2.7 Code HighSpeed", 0.001818, 0.007552, 0, 0.0003636, 0},

		// ── MiMo ──
		{"mimo-v2-pro", "MiMo v2 Pro", 0.001, 0.002, 0, 0, 0},
		{"mimo-v2", "MiMo v2", 0.0005, 0.001, 0, 0, 0},
		{"mimo-v2-flash", "MiMo v2 Flash", 0.0001, 0.0002, 0, 0, 0},
		{"mimo-v2-omni", "MiMo v2 Omni", 0.001, 0.002, 0, 0, 0},
		{"mimo-v2.5", "MiMo v2.5", 0.0008, 0.0016, 0, 0, 0},
		{"mimo-v2.5-pro", "MiMo v2.5 Pro", 0.0015, 0.003, 0, 0, 0},
		{"mimo-v2.5-free", "MiMo v2.5 Free", 0.0005, 0.001, 0, 0, 0},
		{"mimo-auto", "MiMo Auto", 0.0005, 0.001, 0, 0, 0},

		// ── GLM / Zhipu ──
		{"glm-5", "GLM 5", 0.0005, 0.001, 0, 0, 0},
		{"glm-5.1", "GLM 5.1", 0.0005, 0.001, 0, 0, 0},
		{"glm-5.2", "GLM 5.2", 0.0005, 0.001, 0, 0, 0},

		// ── Alibaba Qwen ──
		{"qwen3.5-plus", "Qwen 3.5 Plus", 0.0004, 0.0012, 0, 0, 0},
		{"qwen3.6-plus", "Qwen 3.6 Plus", 0.0004, 0.0012, 0, 0, 0},
		{"qwen3.7-max", "Qwen 3.7 Max", 0.0008, 0.0024, 0, 0, 0},
		{"qwen3.7-plus", "Qwen 3.7 Plus", 0.0004, 0.0012, 0, 0, 0},

		// ── MiniMax ──
		{"minimax-m2.5", "MiniMax M2.5", 0.0005, 0.001, 0, 0, 0},
		{"minimax-m2.7", "MiniMax M2.7", 0.0008, 0.0016, 0, 0, 0},
		{"minimax-m3", "MiniMax M3", 0.001, 0.002, 0, 0, 0},

		// ── Mistral (per 1K tokens) — via Mistral AI / API ──
		{"mistral-large-latest", "Mistral Large Latest", 0.002, 0.006, 0, 0, 0},
		{"mistral-small-latest", "Mistral Small Latest", 0.0001, 0.0003, 0, 0, 0},
		{"pixtral-large-latest", "Pixtral Large Latest", 0.002, 0.006, 0, 0, 0},
		{"codestral-latest", "Codestral Latest", 0.0003, 0.0009, 0, 0, 0},
		{"ministral-3b-latest", "Ministral 3B Latest", 0.00004, 0.00004, 0, 0, 0},
		{"ministral-8b-latest", "Ministral 8B Latest", 0.0001, 0.0001, 0, 0, 0},

		// ── MiniMax ──
		{"minimax-m2.1", "MiniMax M2.1", 0.0003, 0.0012, 0, 0, 0},

		// ── GLM older variants (approximate legacy Zhipu pricing) ──
		{"glm-4", "GLM 4", 0.001, 0.002, 0, 0, 0},
		{"glm-4-plus", "GLM 4 Plus", 0.001, 0.002, 0, 0, 0},
		{"glm-4-flash", "GLM 4 Flash", 0.0001, 0.0002, 0, 0, 0},
		{"glm-4v", "GLM 4V", 0.001, 0.002, 0, 0, 0},
		{"glm-4-9b", "GLM 4 9B", 0.0001, 0.0001, 0, 0, 0},
		{"glm-3-turbo", "GLM 3 Turbo", 0.0005, 0.001, 0, 0, 0},

		// ── Moonshot Kimi ──
		{"kimi-k2-thinking", "Kimi K2 Thinking", 0.0006, 0.0025, 0, 0, 0},

		// ── Amazon Bedrock (bare AWS model IDs; per 1K tokens) ──
		{"anthropic.claude-3-7-sonnet-20250219-v1:0", "Claude 3.7 Sonnet (Bedrock)", 0.003, 0.015, 0, 0, 0},
		{"anthropic.claude-3-5-sonnet-20241022-v2:0", "Claude 3.5 Sonnet v2 (Bedrock)", 0.003, 0.015, 0, 0, 0},
		{"anthropic.claude-3-5-haiku-20241022-v1:0", "Claude 3.5 Haiku (Bedrock)", 0.0008, 0.004, 0, 0, 0},
		{"anthropic.claude-3-opus-20240229-v1:0", "Claude 3 Opus (Bedrock)", 0.015, 0.075, 0, 0, 0},
		{"amazon.nova-pro-v1:0", "Amazon Nova Pro (Bedrock)", 0.0008, 0.0032, 0, 0, 0},
		{"amazon.nova-lite-v1:0", "Amazon Nova Lite (Bedrock)", 0.00006, 0.00024, 0, 0, 0},
		{"meta.llama3-3-70b-instruct-v1:0", "Llama 3.3 70B Instruct (Bedrock)", 0.00265, 0.00265, 0, 0, 0},
		{"deepseek.r1-v1:0", "DeepSeek R1 (Bedrock)", 0.0007, 0.0025, 0, 0, 0},
		{"mistral.mistral-large-2407-v1:0", "Mistral Large 2 (Bedrock)", 0.0005, 0.0015, 0, 0, 0},

		// ── Cerebras (per 1M token rates from public docs) ──
		{"llama-3.1-8b", "Llama 3.1 8B (Cerebras)", 0.0001, 0.0001, 0, 0, 0},
		{"llama-3.1-70b", "Llama 3.1 70B (Cerebras)", 0.0006, 0.0006, 0, 0, 0},
		{"llama-3.3-70b", "Llama 3.3 70B (Cerebras)", 0.0006, 0.0006, 0, 0, 0},

		// ── Together AI (model part after provider prefix) ──
		{"Llama-3.3-70B-Instruct-Turbo", "Llama 3.3 70B Instruct Turbo", 0.00088, 0.00088, 0, 0, 0},
		{"Llama-3.1-8B-Instruct-Turbo", "Llama 3.1 8B Instruct Turbo", 0.00018, 0.00018, 0, 0, 0},
		{"Qwen2.5-72B-Instruct", "Qwen2.5 72B Instruct", 0.0012, 0.0012, 0, 0, 0},
		{"DeepSeek-V3", "DeepSeek V3", 0.00125, 0.00125, 0, 0, 0},

		// ── Fireworks AI (model part after "accounts/") ──
		{"fireworks/models/llama-v3p1-8b-instruct", "Llama 3.1 8B Instruct", 0.0002, 0.0002, 0, 0, 0},
		{"fireworks/models/llama-v3p1-70b-instruct", "Llama 3.1 70B Instruct", 0.0009, 0.0009, 0, 0, 0},

		// ── Novita / Lambda (model part after provider prefix) ──
		{"llama-3.1-8b-instruct", "Llama 3.1 8B Instruct", 0.00002, 0.00005, 0, 0, 0},
		{"llama3.1-8b-instruct", "Llama 3.1 8B Instruct", 0.00002, 0.00003, 0, 0, 0},
		{"llama3.1-70b-instruct", "Llama 3.1 70B Instruct", 0.00012, 0.0003, 0, 0, 0},

		// ── Free-tier (real model price, offered free by some providers) ──

		{"hy3-preview", "HY3 Preview", 0.0005, 0.001, 0, 0, 0},
		{"hy3-free", "HY3 Free", 0.0003, 0.0006, 0, 0, 0},
		{"deepseek-v4-flash-free", "DeepSeek v4 Flash Free", 0.0001, 0.0004, 0, 0, 0},
		{"nemotron-3-ultra-free", "Nemotron 3 Ultra Free", 0.0003, 0.0006, 0, 0, 0},
		{"north-mini-code-free", "North Mini Code Free", 0.0002, 0.0004, 0, 0, 0},

		// ── ZenMux (average canonical model rates; lookup strips the zenmux/ prefix) ──
		{"z-ai/glm-5.2", "GLM 5.2", 0.0014, 0.0044, 0, 0, 0},
		{"deepseek-v3.2", "DeepSeek V3.2", 0.00062, 0.00185, 0, 0, 0},
		{"grok-4.1-fast", "Grok 4.1 Fast", 0.0002, 0.0005, 0, 0, 0},
		{"mistral-large", "Mistral Large", 0.002, 0.006, 0, 0, 0},

		// ── Misc ──
		{"big-pickle", "Big Pickle", 0.0005, 0.001, 0, 0, 0},
}
	// Guard: seed must never contain duplicate model IDs or $0 (free-tier) rows.
	// Every seeded model must carry a real price; duplicates would surface as
	// duplicate cards in the UI. Fail the migration loudly if this is violated.
	if err := validateSeedPricing(seedPricing); err != nil {
		return err
	}
	// One-time reset: wipe any previously-seeded rows so stale IDs / $0 free-tier
	// entries from older builds cannot coexist with the new canonical seed.
	// The pricing table is seed-only (no runtime writes), so this is safe.
	if _, err := db.Exec(`DELETE FROM model_pricing`); err != nil {
		return err
	}
	for _, p := range seedPricing {
		if _, err := db.Exec(`INSERT OR REPLACE INTO model_pricing
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

// validateSeedPricing enforces the model-pricing seed contract:
//   - no duplicate model IDs (would render as duplicate cards)
//   - no $0 input+output rows (every model must carry a real price)
func validateSeedPricing(seed []struct {
	ID, Name                                 string
	In, Out, Reason, CachedRead, CachedWrite float64
},
) error {
	seen := make(map[string]bool, len(seed))
	for _, p := range seed {
		if p.ID == "" {
			return fmt.Errorf("seed pricing: empty model_id for %q", p.Name)
		}
		if seen[p.ID] {
			return fmt.Errorf("seed pricing: duplicate model_id %q", p.ID)
		}
		seen[p.ID] = true
		if p.In == 0 && p.Out == 0 {
			return fmt.Errorf("seed pricing: %q has $0 input+output (free-tier must carry a real price)", p.ID)
		}
	}
	return nil
}

// generateFingerprint returns a 64-character lowercase hex string suitable for
// use as a MiMoCode device fingerprint. It panics only if the OS random
// source fails catastrophically.
func generateFingerprint() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
