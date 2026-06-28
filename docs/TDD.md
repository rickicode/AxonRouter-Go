# AxonRouter-Go — Technical Design Document (TDD)

## 1. Project Structure

```
axonrouter-go/
├── cmd/
│   ├── server/main.go              ← SINGLE ENTRY, single port (3777)
│   └── cli/main.go                 ← CLI (start/stop/status)
├── internal/
│   ├── api/
│   │   ├── router.go               ← SINGLE Gin router, mounts all routes
│   │   ├── middleware/
│   │   │   ├── auth.go             ← API key auth
│   │   │   ├── cors.go             ← CORS
│   │   │   ├── ratelimit.go        ← Rate limiting
│   │   │   └── logging.go          ← Request logging
│   │   └── handlers/
│   │       ├── v1/                 ← /v1/* CHILD SYSTEM (proxy)
│   │       │   ├── chat.go         ← POST /v1/chat/completions
│   │       │   ├── messages.go     ← POST /v1/messages
│   │       │   ├── responses.go    ← POST /v1/responses
│   │       │   ├── models.go       ← GET /v1/models
│   │       │   ├── embeddings.go   ← POST /v1/embeddings
│   │       │   ├── tts.go          ← POST /v1/audio/speech
│   │       │   ├── stt.go          ← POST /v1/audio/transcriptions
│   │       │   ├── images.go       ← POST /v1/images/generations
│   │       │   ├── video.go        ← POST /v1/video/generations
│   │       │   └── unified.go      ← POST /v1/unified
│   │       └── admin/              ← ADMIN handlers (dashboard)
│   │           ├── providers.go    ← CRUD providers
│   │           ├── connections.go  ← CRUD connections (paginated)
│   │           ├── combos.go       ← CRUD combos
│   │           ├── logs.go         ← Request logs (paginated)
│   │           ├── settings.go     ← Settings
│   │           └── dashboard.go    ← Dashboard stats
│   ├── translator/                 ← Format translation (shared)
│   │   ├── registry.go
│   │   ├── formats.go
│   │   ├── openai/ (claude.go, gemini.go, responses.go)
│   │   ├── claude/openai.go
│   │   ├── gemini/openai.go
│   │   ├── codex/responses.go
│   │   ├── antigravity/openai.go
│   │   └── kiro/openai.go
│   ├── auth/                       ← OAuth flows (shared)
│   │   ├── manager.go
│   │   ├── codex/ (oauth.go, token.go)
│   │   ├── antigravity/ (oauth.go, token.go)
│   │   └── kiro/ (oauth.go, token.go)
│   ├── executor/                   ← Provider executors (shared)
│   │   ├── base.go
│   │   ├── openai.go, claude.go, gemini.go
│   │   ├── codex.go, antigravity.go, kiro.go
│   │   ├── tts.go, stt.go, images.go, video.go
│   ├── connstate/                  ← Connection state (shared)
│   │   ├── state.go               ← Status enums
│   │   ├── store.go               ← sync.Map + SQLite WAL
│   │   ├── detector.go            ← Error pattern detection
│   │   ├── eligibility.go         ← Pre-computed eligible list
│   │   └── ratelimit.go           ← TPM/RPM tracking
│   ├── combo/                      ← Combo system (shared)
│   │   ├── combo.go
│   │   ├── rotation.go
│   │   ├── circuit_breaker.go
│   │   ├── fallback.go
│   │   ├── smart_combo.go
│   │   └── default.go
│   ├── usage/                      ← Usage tracking (shared)
│   │   ├── tracker.go             ← Async logging
│   │   ├── aggregator.go
│   │   ├── pricing.go
│   │   └── queries.go
│   ├── db/                         ← SQLite (shared)
│   │   ├── sqlite.go
│   │   ├── models.go
│   │   └── migrations.go
│   ├── config/
│   │   └── config.go
│   └── background/                 ← Background goroutines
│       ├── quota_scheduler.go     ← Quota check (every 5 min)
│       ├── usage_flush.go         ← Usage log flush (every 5 sec)
│       └── cleanup.go             ← Circuit breaker cleanup
├── web/                            ← Svelte frontend (go:embed)
│   ├── src/routes/
│   │   ├── +page.svelte            ← Dashboard home
│   │   ├── providers/              ← Provider management
│   │   ├── combos/                 ← Combo editor
│   │   ├── logs/                   ← Request logs
│   │   └── settings/               ← Settings
│   ├── package.json
│   └── svelte.config.js
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

**Key: Single binary, single port. `/v1/*` handlers are in `internal/api/handlers/v1/` — a clean child system within the same binary.**
│   ├── translator/
│   │   ├── registry.go             ← translator registry (from CLIProxyAPI)
│   │   ├── formats.go              ← format identifiers
│   │   ├── openai/
│   │   │   ├── claude.go           ← openai ↔ claude
│   │   │   ├── gemini.go           ← openai ↔ gemini
│   │   │   └── responses.go        ← openai ↔ openai-responses
│   │   ├── claude/
│   │   │   └── openai.go           ← claude → openai
│   │   ├── gemini/
│   │   │   └── openai.go           ← gemini → openai
│   │   ├── codex/
│   │   │   └── responses.go        ← codex ↔ openai-responses
│   │   ├── antigravity/
│   │   │   └── openai.go           ← antigravity ↔ openai
│   │   └── kiro/
│   │       └── openai.go           ← kiro ↔ openai
│   ├── auth/
│   │   ├── manager.go              ← auth manager
│   │   ├── codex/
│   │   │   ├── oauth.go            ← OpenAI OAuth device code flow
│   │   │   └── token.go            ← token refresh
│   │   ├── antigravity/
│   │   │   ├── oauth.go            ← Google OAuth flow
│   │   │   └── token.go            ← token refresh
│   │   └── kiro/
│   │       ├── oauth.go            ← AWS OAuth flow
│   │       └── token.go            ← token refresh
│   ├── executor/
│   │   ├── base.go                 ← BaseExecutor (from CLIProxyAPI)
│   │   ├── openai.go               ← OpenAI executor
│   │   ├── claude.go               ← Claude executor
│   │   ├── gemini.go               ← Gemini executor
│   │   ├── codex.go                ← Codex executor
│   │   ├── antigravity.go          ← Antigravity executor
│   │   ├── kiro.go                 ← Kiro executor
│   │   ├── tts.go                  ← TTS executor (multi-provider)
│   │   ├── stt.go                  ← STT executor (multi-provider)
│   │   ├── images.go               ← Image generation executor
│   │   └── video.go                ← Video generation executor
│   ├── connstate/
│   │   ├── state.go                ← ConnectionState struct + status enums
│   │   ├── store.go                ← in-memory sync.Map + SQLite WAL persist
│   │   ├── detector.go             ← error pattern detection → status update
│   │   ├── eligibility.go          ← pre-computed eligible list per provider
│   │   └── ratelimit.go            ← model-level TPM/RPM tracking
│   ├── combo/
│   │   ├── combo.go                ← combo handler (from AxonRouter)
│   │   ├── rotation.go             ← round-robin rotation
│   │   ├── circuit_breaker.go      ← circuit breaker per connection
│   │   ├── fallback.go             ← fallback logic
│   │   ├── smart_combo.go          ← smart combo resolution (auto/balanced/economy/premium)
│   │   └── default.go              ← default combo generation
│   ├── usage/
│   │   ├── tracker.go              ← per-request logging (async buffer)
│   │   ├── aggregator.go           ← per-provider aggregation
│   │   ├── pricing.go              ← cost estimation
│   │   └── queries.go              ← DB queries (paginated)
│   ├── db/
│   │   ├── sqlite.go               ← SQLite connection + migrations
│   │   ├── models.go               ← DB models
│   │   └── migrations.go           ← schema migrations
│   └── config/
│       └── config.go               ← app config from SQLite
├── web/                            ← SvelteKit frontend
│   ├── src/
│   │   ├── routes/
│   │   │   ├── +page.svelte        ← dashboard home
│   │   │   ├── +layout.svelte      ← main layout (nav + footer)
│   │   │   ├── providers/
│   │   │   │   ├── +page.svelte    ← provider list (cards with counts)
│   │   │   │   └── [id]/
│   │   │   │       ├── +page.svelte ← provider detail (paginated connections)
│   │   │   │       └── [connId]/
│   │   │   │           └── +page.svelte ← connection detail
│   │   │   ├── combos/
│   │   │   │   ├── +page.svelte    ← combo list
│   │   │   │   └── [id]/
│   │   │   │       └── +page.svelte ← combo editor
│   │   │   ├── logs/
│   │   │   │   └── +page.svelte    ← request logs (paginated)
│   │   │   └── settings/
│   │   │       └── +page.svelte    ← settings
│   │   ├── lib/
│   │   │   ├── api.ts              ← API client
│   │   │   ├── stores.ts           ← Svelte stores
│   │   │   └── components/         ← Shared components
│   │   │       ├── Card.svelte
│   │   │       ├── Button.svelte
│   │   │       ├── Badge.svelte
│   │   │       ├── DataTable.svelte
│   │   │       └── ...            ← More components
│   │   └── app.css                 ← Global styles (Tailwind + design tokens)
│   ├── static/                     ← Static assets
│   ├── build/                      ← Static output (go:embed source)
│   ├── package.json                ← Dependencies (SvelteKit, Vite, Tailwind)
│   ├── svelte.config.js            ← SvelteKit config (static adapter)
│   ├── vite.config.js              ← Vite configuration
│   ├── tailwind.config.js          ← Tailwind config (DESIGN.md tokens)
│   ├── postcss.config.js           ← PostCSS config
│   └── tsconfig.json               ← TypeScript config
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

**Frontend Technology Stack:**
- **Framework:** SvelteKit 2.x with static adapter
- **Build Tool:** Vite 5.x for fast development and optimized production builds
- **Styling:** Tailwind CSS 3.4+ with design token system from DESIGN.md
- **Typography:** Inter (display) + JetBrains Mono (mono caps)
- **State Management:** Svelte stores (writable, derived)
- **Routing:** SvelteKit file-based routing
- **HTTP Client:** Fetch API with custom wrapper (`web/src/lib/api.ts`)
- **Build Output:** Static HTML/JS/CSS in `web/build/` directory
- **Go Embed:** `go:embed web/build/*` in `internal/web/embed.go`

**Design System (from DESIGN.md):**
- **Colors:** Canvas dark (`#010120`) + white (`#ffffff`) alternating surfaces
- **Spacing:** 4px base unit with tokens (xs: 4px, sm: 8px, md: 12px, lg: 16px, xl: 20px, 2xl: 24px, 3xl: 32px)
- **Border Radius:** xs: 3.25px, sm: 4px, md: 8px, full: 9999px
- **Components:** Cards, buttons, badges, data tables, inputs with specific styling
- **Brand Gradient:** Three-color gradient (orange → magenta → periwinkle)
- **Typography:** Mono caps for labels, display sans for headlines

**Frontend Build Process:**
1. `npm run build` in `web/` directory
2. Generates static files in `web/build/`
3. Go binary embeds `web/build/*` via `go:embed`
4. Serves static files via Gin router

---

## 2. SQLite Schema

```sql
-- Providers (tipe provider, bukan instance)
CREATE TABLE provider_types (
    id TEXT PRIMARY KEY,                -- "openai", "claude", "gemini", atau custom name seperti "9router"
    display_name TEXT NOT NULL,
    format TEXT NOT NULL,               -- "openai", "claude", "gemini", "openai-responses", etc
    base_url TEXT NOT NULL,
    is_custom INTEGER DEFAULT 0,        -- 1 untuk custom provider
    custom_headers TEXT,                -- JSON: custom headers untuk custom provider
    created_at INTEGER NOT NULL
);

-- Connections (instance per provider dengan credentials)
CREATE TABLE connections (
    id TEXT PRIMARY KEY,
    provider_type_id TEXT NOT NULL REFERENCES provider_types(id),
    name TEXT NOT NULL,                  -- user-given name
    auth_type TEXT NOT NULL,             -- "api_key", "oauth", "none"
    api_key TEXT,                        -- encrypted
    oauth_token TEXT,                    -- encrypted
    oauth_refresh_token TEXT,            -- encrypted
    oauth_expires_at INTEGER,
    status TEXT NOT NULL DEFAULT 'ready', -- ready/rate_limited/quota_exhausted/balance_empty/auth_failed/suspended/disabled
    cooldown_until INTEGER,              -- auto-recover timestamp
    last_error TEXT,
    last_error_code INTEGER,
    last_success_at INTEGER,
    last_failure_at INTEGER,
    failure_count INTEGER DEFAULT 0,
    capabilities TEXT,                   -- JSON: ["chat", "tts", "stt", "image", "video"]
    is_active INTEGER DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- Model Rate Limits (per model per connection)
CREATE TABLE model_rate_limits (
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

-- API Keys (client auth ke proxy)
CREATE TABLE api_keys (
    id TEXT PRIMARY KEY,
    key_hash TEXT NOT NULL UNIQUE,
    name TEXT,
    rate_limit_per_min INTEGER DEFAULT 600,
    is_active INTEGER DEFAULT 1,
    created_at INTEGER NOT NULL
);

-- Combos
CREATE TABLE combos (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    strategy TEXT NOT NULL DEFAULT 'priority',  -- priority, round-robin
    sticky_limit INTEGER DEFAULT 1,
    timeout_ms INTEGER DEFAULT 30000,
    is_smart INTEGER DEFAULT 0,                  -- 1 for auto/balanced/economy/premium
    smart_goal TEXT,                              -- auto, economy, balanced, premium
    is_active INTEGER DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- Combo Steps
CREATE TABLE combo_steps (
    id TEXT PRIMARY KEY,
    combo_id TEXT NOT NULL REFERENCES combos(id),
    connection_id TEXT NOT NULL REFERENCES connections(id),
    model_id TEXT NOT NULL,
    priority INTEGER NOT NULL,
    weight INTEGER DEFAULT 100,
    created_at INTEGER NOT NULL
);

-- Request Logs (async buffered insert)
CREATE TABLE request_logs (
    id TEXT PRIMARY KEY,
    timestamp INTEGER NOT NULL,
    connection_id TEXT,
    provider_type_id TEXT,
    model_id TEXT,
    combo_id TEXT,
    modality TEXT NOT NULL,                 -- chat, tts, stt, image, video
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    reasoning_tokens INTEGER DEFAULT 0,
    latency_ms INTEGER,
    status_code INTEGER,
    error_message TEXT,
    cost_usd REAL DEFAULT 0,
    created_at INTEGER NOT NULL
);

-- Indexes for pagination + filtering
CREATE INDEX idx_request_logs_timestamp ON request_logs(timestamp DESC);
CREATE INDEX idx_request_logs_provider ON request_logs(provider_type_id, timestamp DESC);
CREATE INDEX idx_request_logs_connection ON request_logs(connection_id, timestamp DESC);
CREATE INDEX idx_request_logs_model ON request_logs(model_id, timestamp DESC);
CREATE INDEX idx_connections_provider ON connections(provider_type_id, status);
CREATE INDEX idx_connections_status ON connections(status);

-- Settings
CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

-- Rotation State (for combo round-robin)
CREATE TABLE rotation_state (
    combo_id TEXT PRIMARY KEY REFERENCES combos(id),
    counter INTEGER DEFAULT 0,
    updated_at INTEGER NOT NULL
);
```

---

## 3. Connection State System

### 3.0 Custom Provider Support

Custom providers (OpenAI-compatible dan Anthropic-compatible) disimpan di `provider_types` table dengan `is_custom = 1`. Nama yang user berikan menjadi ID utama.

**Model ID Format:**
```
<provider-prefix>/<model-id>
```

**Built-in Provider Prefixes:**
```
oc/          → OpenCode Free (openai format, no auth)
oc-zen/      → OpenCode Zen (openai format, API key)
oc-go/       → OpenCode Go (openai format, API key, different endpoint)
mimocode/    → MiMoCode Free (openai format, bootstrap JWT)
mimo/        → MiMo PAYG (openai format, API key)
mimo-tp/     → MiMo Token Plan (openai format, tp-* API key, regional endpoint)
deepseek/    → DeepSeek (openai format, API key)
claude/      → Claude (claude format, API key)
gemini/      → Gemini (gemini format, API key)
cx/          → Codex (openai-responses format, OAuth)  ← BEDA dengan openai!
ag/          → Antigravity (antigravity format, OAuth Google)
kiro/        → Kiro (kiro format, OAuth AWS)
openai/      → OpenAI (openai format, API key)
groq/        → Groq (openai format, API key)
elevenlabs/  → ElevenLabs (openai format, API key)
deepgram/    → Deepgram (openai format, API key)
```

**Contoh perbedaan Codex vs OpenAI:**
```
cx/gpt-5.4       → Codex provider, OAuth auth, openai-responses format
openai/gpt-5.4   → OpenAI provider, API key auth, openai format
```

Ini dua connection berbeda ke dua backend berbeda dengan auth dan format berbeda.

**Custom Provider Prefixes:**
```
9router/           → Custom OpenAI-compatible provider
my-claude-proxy/   → Custom Anthropic-compatible provider
```

Prefix = nama yang user berikan saat add custom provider.

**Routing Resolution:**
```go
func resolveModelID(modelStr string) (providerPrefix string, modelID string) {
    parts := strings.SplitN(modelStr, "/", 2)
    if len(parts) == 2 {
        return parts[0], parts[1]  // "cx/gpt-5.4" → ("cx", "gpt-5.4")
    }
    // No prefix → try to infer from model name
    return inferProviderFromModel(modelStr), modelStr
}
```

**Custom Provider Execution:**
- Custom provider pakai `BaseExecutor` dengan base URL dari DB
- Headers: Authorization Bearer (openai) atau x-api-key (claude) + custom headers dari DB
- Translator: pakai translator yang sesuai dengan format (openai/claude)

```go
func (e *BaseExecutor) buildRequest(conn *Connection, modelID string, body []byte) (*http.Request, error) {
    providerType := getProviderType(conn.ProviderTypeID)
    
    url := providerType.BaseURL + "/chat/completions"  // openai format
    if providerType.Format == "claude" {
        url = providerType.BaseURL + "/messages"
    }
    
    req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
    
    // Auth
    if conn.AuthType == "api_key" {
        if providerType.Format == "claude" {
            req.Header.Set("x-api-key", conn.APIKey)
        } else {
            req.Header.Set("Authorization", "Bearer "+conn.APIKey)
        }
    }
    
    // Custom headers
    if providerType.CustomHeaders != nil {
        for k, v := range providerType.CustomHeaders {
            req.Header.Set(k, v)
        }
    }
    
    return req, nil
}
```

### 3.1 Status Model (Final)

```go
type ConnectionStatus string

const (
    StatusReady           ConnectionStatus = "ready"
    StatusRateLimited     ConnectionStatus = "rate_limited"
    StatusQuotaExhausted  ConnectionStatus = "quota_exhausted"
    StatusBalanceEmpty    ConnectionStatus = "balance_empty"
    StatusAuthFailed      ConnectionStatus = "auth_failed"
    StatusSuspended       ConnectionStatus = "suspended"
    StatusDisabled        ConnectionStatus = "disabled"
)
```

### 3.2 Connection State (In-Memory)

```go
type ConnectionState struct {
    ID              string
    ProviderTypeID  string
    Status          ConnectionStatus
    CooldownUntil   *time.Time
    LastError       string
    LastErrorCode   int
    FailureCount    int
    LastSuccessAt   *time.Time
    LastFailureAt   *time.Time
    
    // Model-level rate limits
    ModelLimits     sync.Map  // modelID → *ModelLimitState
}

type ModelLimitState struct {
    ModelID         string
    TPMRemaining    int
    TPMLimit        int
    RPMRemaining    int
    RPMLimit        int
    CooldownUntil   *time.Time
    LastUpdatedAt   time.Time
}
```

### 3.3 Eligibility Manager (Pre-Computed)

```go
type EligibilityManager struct {
    // Pre-computed eligible lists per provider, updated async
    eligible sync.Map  // providerTypeID → atomic.Value([]*Connection)
    
    // All connection states
    states sync.Map  // connectionID → *ConnectionState
}

// Routing call: O(1) — just pick from pre-computed list
func (em *EligibilityManager) PickConnection(providerTypeID string, modelID string) *Connection {
    list := em.eligible.Load(providerTypeID)
    if list == nil { return nil }
    
    eligible := list.([]*Connection)
    for _, conn := range eligible {
        state := em.states.Load(conn.ID)
        
        // Check model-level rate limit
        if ml, ok := state.ModelLimits.Load(modelID); ok {
            if ml.(*ModelLimitState).IsCooldown() {
                continue
            }
        }
        
        return conn
    }
    
    // All rate-limited, pick earliest recoverable
    return em.getEarliestRecoverable(eligible, modelID)
}

// State update: trigger async re-compute
func (em *EligibilityManager) UpdateStatus(connID string, status ConnectionStatus) {
    state := em.states.Load(connID)
    state.Status = status
    em.recomputeEligible(state.ProviderTypeID)  // async
}
```

### 3.4 Error Detector

```go
type Detection struct {
    Status       ConnectionStatus
    CooldownUntil *time.Time
    Scope        string  // "connection" or "model"
    ModelID      string  // only for model-level
    Message      string
}

func DetectError(status int, body string, headers http.Header, modelID string) *Detection {
    // 1. Rate limit (429) → model-level
    if status == 429 {
        cooldown := parseRetryAfterHeader(headers)
        if cooldown == nil { d := 60 * time.Second; cooldown = &d }
        return &Detection{
            Status:       StatusRateLimited,
            CooldownUntil: timePtr(time.Now().Add(*cooldown)),
            Scope:        "model",
            ModelID:      modelID,
        }
    }
    
    // 2. Quota exhausted → connection-level
    if matchesPatterns(body, QUOTA_EXHAUSTED_PATTERNS) {
        resetAt := parseQuotaResetTime(body, headers)
        return &Detection{
            Status:       StatusQuotaExhausted,
            CooldownUntil: resetAt,
            Scope:        "connection",
        }
    }
    
    // 3. Balance empty → connection-level, no auto-recover
    if status == 402 && matchesPatterns(body, BALANCE_EXHAUSTED_PATTERNS) {
        return &Detection{
            Status: StatusBalanceEmpty,
            Scope:  "connection",
        }
    }
    
    // 4. Auth failed → connection-level, no auto-recover
    if status == 401 && matchesPatterns(body, AUTH_FAILED_PATTERNS) {
        return &Detection{
            Status: StatusAuthFailed,
            Scope:  "connection",
        }
    }
    
    // 5. Suspended → connection-level, no auto-recover
    if status == 403 && matchesPatterns(body, SUSPENDED_PATTERNS) {
        return &Detection{
            Status: StatusSuspended,
            Scope:  "connection",
        }
    }
    
    // 6. Transient → circuit breaker
    if status >= 500 {
        return nil  // handled by circuit breaker
    }
    
    return nil
}
```

### 3.5 Detection Patterns

```go
var QUOTA_EXHAUSTED_PATTERNS = []string{
    "exceeded your current quota",
    "quota exceeded",
    "quota exhausted",
    "weekly quota exhausted",
    "monthly quota exceeded",
    "rate limit reached for the current billing period",
    "usage limit has been reached",
}

var BALANCE_EXHAUSTED_PATTERNS = []string{
    "insufficient quota",
    "billing hard limit",
    "hard limit reached",
    "balance insufficient",
    "insufficient funds",
    "payment required",
    "add credits",
    "top up",
    "recharge",
}

var AUTH_FAILED_PATTERNS = []string{
    "invalid api key",
    "invalid apiKey",
    "api key not found",
    "authentication failed",
    "incorrect api key",
    "invalid authentication",
}

var SUSPENDED_PATTERNS = []string{
    "account suspended",
    "account disabled",
    "account banned",
    "account terminated",
    "service unavailable for this account",
}
```

### 3.6 Quota System (Verified from Codebase)

**Source:** OmniRoute `src/lib/usage/fetcher.ts` — `getUsageForProvider()`, AxonRouter `src/lib/usageStatus.ts`

#### Provider Quota Capabilities

| Provider | Proactive Quota API | Reactive Detection | Reset Time | Source |
|----------|--------------------|--------------------|------------|--------|
| **Codex** | ✅ `getCodexUsage()` | ✅ 429 + error text | ✅ 5h + weekly | OmniRoute fetcher.ts |
| **Antigravity** | ✅ `getAntigravityUsage()` | ✅ 429 + error text | ✅ per-family | OmniRoute fetcher.ts |
| **Claude** | ✅ `getClaudeUsage()` | ✅ 429 + error text | ✅ plan-window | OmniRoute fetcher.ts |
| **Gemini CLI** | ✅ `getGeminiUsage()` | ✅ 429 + error text | ✅ from headers | OmniRoute fetcher.ts |
| **GitHub** | ✅ `getGitHubUsage()` | ✅ 429 | ✅ monthly | OmniRoute fetcher.ts |
| **Kiro** | ✅ `getKiroUsage()` | ✅ 429 + error text | ✅ from response | OmniRoute fetcher.ts |
| **Qwen** | ✅ `getQwenUsage()` | ✅ 429 | ❌ unknown | OmniRoute fetcher.ts |
| **Qoder** | ✅ `getQoderUsage()` | ✅ 429 | ❌ unknown | OmniRoute fetcher.ts |
| **OpenAI** | ❌ | ✅ 429 + error text | ❌ | — |
| **Mimo** | ❌ | ✅ 429 + error text | ❌ | — |
| **DeepSeek** | ❌ | ✅ 429 + error text | ❌ | — |
| **Groq** | ❌ | ✅ 429 + error text | ❌ | — |
| **OpenCode** | ❌ | ✅ 429 | ❌ | — |

#### Quota Check Strategy

```go
// Provider quota capability
type QuotaCapability struct {
    HasProactiveCheck bool    // bisa cek quota via API
    QuotaEndpoint     string  // endpoint untuk cek quota
    HasResetTime      bool    // tau kapan quota reset
    ResetDuration     time.Duration  // durasi reset (daily, weekly, monthly)
}

// Verified dari codebase (OmniRoute fetcher.ts)
var PROVIDER_QUOTA_CAPABILITIES = map[string]*QuotaCapability{
    "codex": {
        HasProactiveCheck: true,
        HasResetTime:      true,
        ResetDuration:     7 * 24 * time.Hour,  // weekly
    },
    "antigravity": {
        HasProactiveCheck: true,
        HasResetTime:      true,
        ResetDuration:     24 * time.Hour,  // daily (per-family)
    },
    "claude": {
        HasProactiveCheck: true,
        HasResetTime:      true,
        ResetDuration:     60 * time.Second,  // plan-window
    },
    "gemini-cli": {
        HasProactiveCheck: true,
        HasResetTime:      true,
    },
    "github": {
        HasProactiveCheck: true,
        HasResetTime:      true,
        ResetDuration:     30 * 24 * time.Hour,  // monthly
    },
    "kiro": {
        HasProactiveCheck: true,
        HasResetTime:      true,
    },
    "qwen": {
        HasProactiveCheck: true,
        HasResetTime:      false,
    },
    "qoder": {
        HasProactiveCheck: true,
        HasResetTime:      false,
    },
    // Providers tanpa proactive quota check:
    "openai":    { HasProactiveCheck: false },
    "mimo":      { HasProactiveCheck: false },
    "deepseek":  { HasProactiveCheck: false },
    "groq":      { HasProactiveCheck: false },
    "opencode":  { HasProactiveCheck: false },
}
```

#### Background Quota Scheduler

```go
// Reference: OmniRoute domain/quotaCache.ts — background refresh every 1 minute
func (q *QuotaChecker) StartScheduler(interval time.Duration) {
    ticker := time.NewTicker(interval)  // default 30 min, configurable
    for range ticker.C {
        for _, conn := range q.getAllConnections() {
            cap := PROVIDER_QUOTA_CAPABILITIES[conn.ProviderType]
            if cap == nil || !cap.HasProactiveCheck {
                continue  // skip provider tanpa quota endpoint
            }
            
            quota := q.CheckQuota(conn)
            if quota != nil {
                q.updateQuotaState(conn.ID, quota)
            }
        }
    }
}
```

#### Error Text Patterns (Verified from AxonRouter usageStatus.ts)

```go
// Dari AxonRouter src/lib/usageStatus.ts line 16-28
var QUOTA_EXHAUSTED_PATTERNS = []string{
    "exceeded your current quota",
    "quota exceeded",
    "quota exhausted",
    "weekly quota exhausted",
    "monthly quota exceeded",
    "rate limit reached for the current billing period",
    "usage limit has been reached",
}
```

---

## 4. Core Flow: Chat Completions

```
POST /v1/chat/completions
  │
  ├─ 1. Auth middleware: validate API key
  ├─ 2. Rate limit middleware: check rate limit
  ├─ 3. Parse body → extract model string
  │
  ├─ 4. Is smart combo? (auto/balanced/economy/premium)
  │   └─ Yes → resolveSmartCombo() → select combo based on goal
  │
  ├─ 5. Is combo? (model matches combo name)
  │   └─ Yes → handleComboChat()
  │       ├─ Flatten combo steps
  │       ├─ For each step:
  │       │   ├─ PickConnection(provider, model) ← O(1) from pre-computed list
  │       │   ├─ Check model-level rate limit
  │       │   ├─ translateRequest(source → openai → target)
  │       │   ├─ executor.execute()
  │       │   ├─ On success: return response, log usage
  │       │   ├─ On error: DetectError() → update state
  │       │   └─ Try next step
  │       └─ All failed: return error
  │
  └─ 6. Single model → handleSingleModel()
      ├─ PickConnection(provider, model) ← O(1)
      ├─ translateRequest(sourceFormat → openai → targetFormat)
      ├─ executor.execute()
      ├─ On error: DetectError() → update state → retry with next connection
      ├─ translateResponse(targetFormat → openai → sourceFormat)
      └─ Log usage → return response
```

---

## 5. Circuit Breaker

```go
type CircuitBreaker struct {
    State         CircuitState
    FailureCount  int
    SuccessCount  int
    LastFailureAt time.Time
    OpenedAt      time.Time
}

type CircuitState string

const (
    CB_CLOSED    CircuitState = "closed"
    CB_OPEN      CircuitState = "open"
    CB_HALF_OPEN CircuitState = "half_open"
)

func (cb *CircuitBreaker) CanExecute() bool {
    switch cb.State {
    case CB_CLOSED:
        return true
    case CB_OPEN:
        if time.Since(cb.OpenedAt) > 60*time.Second {
            cb.State = CB_HALF_OPEN
            return true
        }
        return false
    case CB_HALF_OPEN:
        return true
    }
    return true
}

func (cb *CircuitBreaker) RecordSuccess() {
    if cb.State == CB_HALF_OPEN {
        cb.SuccessCount++
        if cb.SuccessCount >= 2 {
            cb.State = CB_CLOSED
            cb.FailureCount = 0
            cb.SuccessCount = 0
        }
    }
}

func (cb *CircuitBreaker) RecordFailure() {
    cb.FailureCount++
    cb.LastFailureAt = time.Now()
    if cb.State == CB_HALF_OPEN {
        cb.State = CB_OPEN
        cb.OpenedAt = time.Now()
    } else if cb.FailureCount >= 3 {
        cb.State = CB_OPEN
        cb.OpenedAt = time.Now()
    }
}
```

---

## 6. Combo System

### 6.1 Combo Resolution

```
modelStr → isSmartCombo()? → resolveSmartCombo() → combo name
         → isCombo()? → getComboModels() → model steps
         → isSingleModel()? → handleSingleModel()
```

### 6.2 Smart Combo Resolution

```go
func resolveSmartCombo(goal string, combos []*Combo, telemetry *Telemetry) *Combo {
    switch goal {
    case "auto":
        return resolveAutoGoal(combos, telemetry)
    case "economy":
        return chooseComboForGoal(combos, "economy")
    case "balanced":
        return chooseComboForGoal(combos, "balanced")
    case "premium":
        return chooseComboForGoal(combos, "premium")
    }
    return nil
}

func resolveAutoGoal(combos []*Combo, telemetry *Telemetry) *Combo {
    // High error rate → escalate to premium
    if telemetry.ErrorRate >= 0.15 || telemetry.FallbackRate >= 0.2 {
        return chooseComboForGoal(combos, "premium")
    }
    // High cost → shift to economy
    if telemetry.TotalCost >= 50 {
        return chooseComboForGoal(combos, "economy")
    }
    // Default → balanced
    return chooseComboForGoal(combos, "balanced")
}
```

### 6.3 Round-Robin Rotation

```go
func getRotatedSteps(steps []*ComboStep, comboName string, strategy string, stickyLimit int) []*ComboStep {
    if strategy != "round-robin" || len(steps) <= 1 {
        return steps
    }
    
    state := getRotationState(comboName)
    effectiveIndex := (state.Counter / stickyLimit) % len(steps)
    state.Counter++
    saveRotationState(comboName, state)
    
    rotated := make([]*ComboStep, len(steps))
    copy(rotated, steps)
    for i := 0; i < effectiveIndex; i++ {
        rotated = append(rotated[1:], rotated[0])
    }
    return rotated
}
```

---

## 7. Async Usage Logging

```go
type UsageLogger struct {
    buffer   chan *RequestLog
    db       *sql.DB
    flushInterval time.Duration
    batchSize int
}

func NewUsageLogger(db *sql.DB) *UsageLogger {
    ul := &UsageLogger{
        buffer:        make(chan *RequestLog, 10000),
        db:            db,
        flushInterval: 5 * time.Second,
        batchSize:     100,
    }
    go ul.flushLoop()
    return ul
}

// Non-blocking: just enqueue
func (ul *UsageLogger) Log(entry *RequestLog) {
    select {
    case ul.buffer <- entry:
    default:
        // Buffer full, drop oldest or log warning
    }
}

// Background flush
func (ul *UsageLogger) flushLoop() {
    ticker := time.NewTicker(ul.flushInterval)
    batch := make([]*RequestLog, 0, ul.batchSize)
    
    for {
        select {
        case entry := <-ul.buffer:
            batch = append(batch, entry)
            if len(batch) >= ul.batchSize {
                ul.flushBatch(batch)
                batch = batch[:0]
            }
        case <-ticker.C:
            if len(batch) > 0 {
                ul.flushBatch(batch)
                batch = batch[:0]
            }
        }
    }
}

func (ul *UsageLogger) flushBatch(batch []*RequestLog) {
    tx, _ := ul.db.Begin()
    stmt, _ := tx.Prepare("INSERT INTO request_logs (...) VALUES (...)")
    for _, entry := range batch {
        stmt.Exec(entry.Timestamp, entry.ConnectionID, ...)
    }
    stmt.Close()
    tx.Commit()
}
```

---

## 8. API Endpoints (Admin)

### Provider Endpoints
```
GET    /api/admin/providers                                    ← list providers with connection counts
GET    /api/admin/providers/:id                                ← provider detail with paginated connections
POST   /api/admin/providers                                    ← add custom provider
PATCH  /api/admin/providers/:id                                ← update provider (name, url, headers)
DELETE /api/admin/providers/:id                                ← delete custom provider (only if no connections)
POST   /api/admin/providers/:id/connections                    ← add connection(s)
POST   /api/admin/providers/:id/connections/bulk               ← bulk add connections
POST   /api/admin/providers/:id/test                           ← test all connections
PATCH  /api/admin/connections/:id                              ← update connection
DELETE /api/admin/connections/:id                              ← delete connection
POST   /api/admin/connections/:id/test                         ← test connection
POST   /api/admin/connections/:id/reset                        ← reset status to ready
PATCH  /api/admin/connections/bulk                              ← bulk update (disable/enable/test)
```

### Custom Provider Add Request
```json
POST /api/admin/providers
{
  "name": "9router",
  "format": "openai",
  "base_url": "https://api.9router.com/v1",
  "auth_type": "api_key",
  "custom_headers": {
    "X-Custom-Header": "value"
  }
}
```

Response:
```json
{
  "id": "9router",
  "display_name": "9router",
  "format": "openai",
  "base_url": "https://api.9router.com/v1",
  "is_custom": true,
  "connection_count": 0
}
```

### Pagination Format
```json
{
  "data": [...],
  "pagination": {
    "page": 1,
    "per_page": 50,
    "total": 847,
    "total_pages": 17
  }
}
```

### Connection List Query Params
```
GET /api/admin/providers/openai/connections?page=1&per_page=50&status=ready&search=key-001
```

---

## 9. Reference File Map

| Component | CLIProxyAPI (Go) | AxonRouter (TS) |
|-----------|-----------------|-----------------|
| **Translator registry** | `internal/translator/translator/translator.go` | `open-sse/translator/index.ts` |
| **OpenAI → Claude** | `internal/translator/openai/claude/` | `translator/request/openai-to-claude.ts` |
| **OpenAI → Gemini** | `internal/translator/openai/gemini/` | `translator/request/openai-to-gemini.ts` |
| **Claude → OpenAI** | `internal/translator/claude/openai/` | `translator/request/claude-to-openai.ts` |
| **Gemini → OpenAI** | `internal/translator/gemini/openai/` | `translator/request/gemini-to-openai.ts` |
| **Codex → OpenAI** | `internal/translator/codex/openai/` | `translator/response/openai-responses.tsx` |
| **Antigravity** | `internal/translator/antigravity/` | `translator/request/antigravity-to-openai.ts` |
| **Kiro** | ❌ | `translator/request/openai-to-kiro.ts` |
| **Auth: Codex** | `internal/auth/codex/` | `src/lib/oauth/` |
| **Auth: Antigravity** | `internal/auth/antigravity/` | `src/lib/oauth/` |
| **Executor base** | `internal/runtime/executor/` | `open-sse/executors/base.ts` |
| **Combo handler** | ❌ | `open-sse/services/combo.tsx` |
| **Smart combo** | ❌ | `src/lib/routing/virtualModelResolver.ts` |
| **Circuit breaker** | ❌ | `open-sse/services/circuitBreaker.ts` |
| **Account state** | ❌ | `src/lib/usageStatus.ts` |
| **Eligibility** | ❌ | `src/lib/providerEligibility.ts` |
| **Connection state** | ❌ | `src/lib/providerHotState.ts` |
| **Usage tracking** | ❌ | `src/lib/usageDb/` |
| **TTS** | ❌ | `open-sse/handlers/ttsCore.tsx` |
| **STT** | ❌ | `open-sse/handlers/sttCore.ts` |
| **Image gen** | `internal/config/disable_image_generation_mode.go` | `src/sse/handlers/imageGenerationCore.ts` |
| **Unified** | ❌ | `src/lib/routing/unifiedContract.ts` |
| **Dashboard** | ❌ (separate repo) | `src/app/(dashboard)/app/` |
