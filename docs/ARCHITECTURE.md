# AxonRouter-Go Architecture

## Overview

AxonRouter-Go adalah universal API proxy untuk coding agents. Dirancang sebagai single Go binary dengan embedded SQLite dan Svelte dashboard.

## Design Principles

1. **Single Binary** — Zero external dependencies, semua embedded
2. **O(1) Routing** — Eligibility snapshot, <1ms regardless of connection count
3. **Fail-Safe** — Circuit breaker, auto-recovery, graceful degradation
4. **Observable** — Per-request logging, usage tracking, dashboard

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Client Request                            │
│                    (OpenAI/Claude/Gemini format)             │
└──────────────────────────┬──────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                    Gin Router                               │
│  ┌─────────────────┐  ┌─────────────────────────────────┐  │
│  │   Middleware     │  │                                 │  │
│  │  - Auth (bcrypt) │  │  Route Matching                 │  │
│  │  - Rate Limit    │  │  /v1/* → Proxy Handlers         │  │
│  │  - CORS          │  │  /api/admin/* → Admin Handlers  │  │
│  │  - Logging       │  │                                 │  │
│  └─────────────────┘  └─────────────────────────────────┘  │
└──────────────────────────┬──────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                    Proxy Handler                             │
│                                                             │
│  1. Parse request (model, format)                           │
│  2. Combo-first routing (if model matches combo)            │
│  3. Direct routing via eligibility snapshot                  │
│  4. OAuth refresh (if needed)                               │
│  5. Translate request (client → provider format)            │
│  6. Execute request                                         │
│  7. Parse rate limit headers                                │
│  8. Translate response (provider → client format)           │
│  9. Log usage                                               │
└──────────────────────────┬──────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                    Connection State                          │
│                                                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐  │
│  │  Connection   │  │   Circuit    │  │  Model Rate      │  │
│  │  State        │  │   Breaker    │  │  Limit           │  │
│  │  (sync.Map)   │  │  (per-conn)  │  │  (per-model)     │  │
│  └──────────────┘  └──────────────┘  └──────────────────┘  │
│                                                             │
│  Layer 1: Connection status (ready/rate_limited/etc)        │
│  Layer 2: Circuit breaker (CLOSED→OPEN→HALF_OPEN)           │
│  Layer 3: Model-level TPM/RPM tracking                      │
└─────────────────────────────────────────────────────────────┘
```

## Package Structure

### `internal/api/`

HTTP layer — Gin handlers, middleware, routing.

- `handlers/v1/` — Proxy endpoints (chat, messages, responses, models, etc.)
- `handlers/admin/` — Admin endpoints (providers, connections, combos, logs)
- `middleware/` — Auth (bcrypt), rate limiting, CORS, logging
- `router.go` — Route registration, dependency injection

### `internal/translator/`

Format translation layer — hub-and-spoke via OpenAI format.

- `registry/` — Global translator registry
- `types/` — Format identifiers (openai, claude, gemini, etc.)
- `openai/` — OpenAI → provider translators
- `claude/` — Claude → OpenAI translator
- `gemini/` — Gemini → OpenAI translator
- `codex/` — Codex → OpenAI translator
- `antigravity/` — Antigravity → OpenAI translator
- `kiro/` — Kiro → OpenAI translator

**Translation Flow:**
```
Client Request (any format)
    ↓
Translate to OpenAI format (if needed)
    ↓
Translate to Provider format
    ↓
Execute request
    ↓
Translate response to OpenAI format
    ↓
Translate to Client format (if needed)
```

### `internal/auth/`

OAuth flows for provider authentication.

- `manager.go` — Central auth manager with singleflight dedup
- `codex/` — Codex OAuth (device code flow)
- `antigravity/` — Antigravity OAuth (Google)
- `kiro/` — Kiro OAuth (AWS Cognito)

**Token Refresh Flow:**
```
401 detected
    ↓
Check if OAuth provider
    ↓
RefreshToken() via singleflight
    ↓
Update in-memory + SQLite
    ↓
Retry request with new token
```

### `internal/executor/`

Provider-specific request execution.

- `base.go` — Base executor with HTTP client
- `openai.go` — OpenAI executor (chat, embeddings, TTS, STT, images, video)
- `claude.go` — Claude executor (messages, count_tokens)
- `gemini.go` — Gemini executor
- `registry.go` — Executor registry

### `internal/connstate/`

Connection state management — the core routing engine.

- `store.go` — In-memory sync.Map store
- `state.go` — Connection state, ModelLimitState
- `eligibility.go` — Eligibility snapshot (O(1) routing)
- `circuit_breaker.go` — Circuit breaker (3/2/60s)
- `detector.go` — Error classification
- `patterns.go` — Error patterns (rate_limit, quota, balance_empty, etc.)
- `headers.go` — Rate limit header parsing

**3-Layer Defense:**
```
Layer 1: Connection State (sync.Map)
    - ready, rate_limited, quota_exhausted, balance_empty, auth_failed, suspended, disabled

Layer 2: Circuit Breaker (per-connection)
    - CLOSED → OPEN (3 failures) → HALF_OPEN (60s) → CLOSED (2 successes)

Layer 3: Model Rate Limit (per model per connection)
    - TPM/RPM tracking from response headers
    - Cooldown timers
```

**Eligibility Snapshot:**
```go
// Pre-computed eligible list, updated async when state changes
type EligibilityManager struct {
    eligible map[string][]string  // prefix → []connID
    store    *Store
}

// O(1) routing — pick from pre-computed list
func (e *EligibilityManager) PickConnection(prefix, modelID string) *ConnectionState {
    conns := e.GetByPrefix(prefix)
    for _, connID := range conns {
        cs := e.store.Get(connID)
        if cs != nil && !cs.IsInCooldown() && !cs.IsModelInCooldown(modelID) {
            return cs
        }
    }
    return nil
}
```

### `internal/combo/`

Combo routing system.

- `handler.go` — Combo resolution, step execution
- `rotation.go` — Round-robin rotation
- `smart.go` — Smart combo (auto, economy, balanced, premium)

**Combo Flow:**
```
Model: "balanced"
    ↓
Resolve combo → [mimo/mimo-v2-pro, cx/gpt-5.4, oc/gpt-4o]
    ↓
Try step 1: mimo/mimo-v2-pro
    ↓ (if failed)
Try step 2: cx/gpt-5.4
    ↓ (if failed)
Try step 3: oc/gpt-4o
    ↓ (if all failed)
Return 503
```

### `internal/usage/`

Usage tracking and logging.

- `tracker.go` — Async buffered logger
- `models.go` — Log entry models

**Async Logging:**
```
Request → Log entry → Buffer (memory)
    ↓ (every 5s or 100 entries)
Batch insert → SQLite
```

### `internal/db/`

SQLite database layer.

- `models.go` — Data models
- `migrations.go` — Schema migrations
- `key_migration.go` — Bcrypt key migration

### `internal/background/`

Background goroutines.

- `quota_scheduler.go` — Cooldown recovery (30 min interval)
- `usage_flush.go` — Usage log flush (5s interval)
- `cleanup.go` — General cleanup (5 min interval)

## Data Flow

### Proxy Request Flow

```
1. Client → POST /v1/chat/completions
2. Auth middleware → validate API key (bcrypt)
3. Rate limit middleware → check per-key/per-IP limit
4. ChatCompletions handler:
   a. Parse body, extract model
   b. Check combo resolution
   c. Get connection via eligibility snapshot (O(1))
   d. Check OAuth expiry, refresh if needed
   e. Translate request (OpenAI → provider format)
   f. Execute request
   g. Parse rate limit headers
   h. Detect errors, update connection state
   i. Translate response (provider → OpenAI format)
   j. Log usage (async)
   k. Return response
```

### Error Detection Flow

```
Response received
    ↓
Check HTTP status code
    ↓
429 → rate_limited (model-level, auto-recover via Retry-After)
402 → Check body patterns:
    - "insufficient funds" → balance_empty (manual)
    - "quota exceeded" → quota_exhausted (auto-recover)
401 → auth_failed (manual)
403 → suspended (manual)
500+ → circuit breaker (auto-recover after 60s)
```

## Frontend Architecture

SvelteKit dengan static adapter untuk `go:embed`.

```
web/
├── src/
│   ├── routes/
│   │   ├── +page.svelte           ← Home (dashboard overview)
│   │   ├── providers/
│   │   │   ├── +page.svelte       ← Provider list
│   │   │   └── [id]/
│   │   │       ├── +page.svelte   ← Provider detail
│   │   │       └── [connId]/
│   │   │           └── +page.svelte ← Connection detail
│   │   ├── combos/
│   │   │   └── +page.svelte       ← Combo management
│   │   ├── logs/
│   │   │   └── +page.svelte       ← Request logs
│   │   └── settings/
│   │       └── +page.svelte       ← Settings
│   ├── lib/
│   │   ├── components/            ← Shared components
│   │   ├── stores/                ← Svelte stores
│   │   └── utils/                 ← Utilities
│   ├── app.css                    ← Global styles
│   └── app.html                   ← HTML template
├── build/                         ← Static output (embedded)
├── package.json
├── svelte.config.js
├── vite.config.js
└── tailwind.config.js
```

## Performance Characteristics

| Metric | Target | Implementation |
|--------|--------|----------------|
| Routing latency | <1ms | Eligibility snapshot (O(1)) |
| Proxy overhead | <5ms | Minimal middleware chain |
| Concurrent streams | 1000+ | Goroutine per request |
| Memory (idle) | <100MB | sync.Map + SQLite WAL |
| Memory (5000 conn) | <500MB | Compact state structs |
| Startup time | <1s | Embedded assets, no external deps |

## Security

1. **API Key Auth** — bcrypt hashed keys, auto-migration on startup
2. **Admin Auth** — X-Admin-Key header
3. **Rate Limiting** — Per-key or per-IP
4. **Constant-Time Comparison** — API key validation
5. **Fail-Closed** — No keys configured = deny all requests
