# AxonRouter-Go — Product Requirements Document (PRD)

## 1. Overview

**AxonRouter-Go** adalah universal API proxy untuk coding agents (Claude Code, Codex CLI, Cursor, Kiro, dll). Client mengirim request dalam format standar OpenAI/Claude/Gemini, proxy menerjemahkan ke format provider tujuan, mengelola credentials, quota, combo routing, dan menyediakan web dashboard untuk manajemen.

**Goal:** Versi sangat stabil — single Go binary, single port, embedded Svelte dashboard, SQLite storage, zero external dependencies.

**Architecture: Single Binary, Internal Separation**
```
┌─────────────────────────────────────────────────────────────┐
│              SINGLE BINARY (port 3777)                        │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │                    Gin Router                          │ │
│  │                                                        │ │
│  │  /v1/* routes              /api/admin/* routes         │ │
│  │  ┌──────────────────┐     ┌──────────────────────────┐ │ │
│  │  │ Proxy Handlers   │     │ Admin Handlers           │ │ │
│  │  │ - chat           │     │ - providers CRUD         │ │ │
│  │  │ - messages       │     │ - connections CRUD       │ │ │
│  │  │ - responses      │     │ - combos CRUD            │ │ │
│  │  │ - models         │     │ - logs (paginated)       │ │ │
│  │  │ - embeddings     │     │ - settings               │ │ │
│  │  │ - audio/tts/stt  │     │ - dashboard stats        │ │ │
│  │  │ - images/video   │     │                          │ │ │
│  │  │ - unified        │     │ Dashboard UI (Svelte)    │ │ │
│  │  └──────────────────┘     │ via go:embed             │ │ │
│  │                           └──────────────────────────┘ │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │                  Shared Internal Packages               │ │
│  │  translator │ auth │ executor │ connstate │ combo       │ │
│  │  usage │ config                                         │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │              Background Goroutines                      │ │
│  │  - Quota scheduler (default 30 min, configurable in settings)                       │ │
│  │  - Usage log flush (every 5 sec)                       │ │
│  │  - Circuit breaker cleanup (every 5 min)               │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │              SQLite (WAL mode)                          │ │
│  └────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

**Tech Stack:**
- Backend: Go + Gin + SQLite
- Frontend: SvelteKit + Vite + Tailwind CSS (embedded via `go:embed`)
- CLI: Minimal — service management + status only
- Config: SQLite (bukan YAML/file-based)

**Frontend Technology Details:**
- **Framework:** SvelteKit with static adapter for go:embed
- **Build Tool:** Vite 5+ for fast development and optimized builds
- **Styling:** Tailwind CSS 3.4+ with design token system
- **Typography:** Inter (display) + JetBrains Mono (mono)
- **Design System:** Based on DESIGN.md (Together AI brand)
- **Output:** Static HTML/JS/CSS in `web/build/` directory

**Internal Separation (clean architecture):**
- `internal/proxy/` — /v1/* handlers, proxy-specific middleware
- `internal/admin/` — /api/admin/* handlers, dashboard, background tasks
- `internal/shared/` — translator, auth, executor, combo, connstate, usage, db
- `internal/web/` — Svelte frontend (embedded)

**Reference Codebases:**
- CLIProxyAPI (Go) — translator, auth, executor
- AxonRouter (TS) — combo system, dashboard, usage tracking

**Frontend Architecture:**
- `web/src/routes/` — SvelteKit pages (file-based routing)
- `web/src/lib/` — Shared components, stores, utilities
- `web/src/app.css` — Global styles with Tailwind + design tokens
- `web/tailwind.config.js` — Tailwind configuration with DESIGN.md tokens
- `web/build/` — Static output (embedded in Go binary via `go:embed`)

**Design System (from DESIGN.md):**
- Colors: Canvas dark (`#010120`) + white (`#ffffff`) alternating surfaces
- Typography: Inter (display) + JetBrains Mono (mono caps)
- Components: Cards, buttons, badges with specific styling
- Layout: 1280px max-width, 4px spacing system
- Brand Gradient: Three-color gradient (orange → magenta → periwinkle)

---

## 2. Scale Assumptions

**PENTING:** Sistem harus dirancang untuk ratusan hingga ribuan akun per provider.

| Metric | Expected | Design Impact |
|--------|----------|---------------|
| Connections per provider | 100-1000+ | Routing harus O(1) atau O(log n), bukan O(n) |
| Total connections | 1000-5000+ | In-memory state harus compact |
| Active requests | 100-1000+ concurrent | Goroutine per request, minimal blocking |
| Providers | 10-20 | Manageable |
| Models per provider | 5-50 | Small |

**Design Implications:**
- **Routing:** Pre-filter eligible connections ke sorted list (by latency/cost), routing tinggal pick first N
- **State:** In-memory `sync.Map` untuk hot state, SQLite WAL untuk persist
- **Dashboard:** Pagination wajib, search/filter per provider, bulk operations
- **Connection list:** Group by provider, show count per status, expandable
- **Logs:** Async write, buffered batch insert

---

## 3. Target Users

- Developer yang pakai coding agents
- Ingin unified endpoint untuk akses multiple AI providers
- Ingin manage API keys, combo routing, quota tracking dari satu dashboard
- Punya banyak akun per provider (100-1000+)

---

## 4. Core Features

### 4.1 Proxy Engine — All Endpoints

| Endpoint | Format | Description | Reference |
|----------|--------|-------------|-----------|
| `POST /v1/chat/completions` | OpenAI Chat | Chat completion (main) | AxonRouter `chat/completions/route.ts` |
| `POST /v1/messages` | Claude | Anthropic Messages API | AxonRouter `messages/route.ts` |
| `POST /v1/responses` | OpenAI Responses | Codex Responses API | AxonRouter `responses/route.ts` |
| `GET /v1/models` | — | Model listing + combos + virtual models | AxonRouter `models/route.ts` |
| `POST /v1/embeddings` | OpenAI | Embeddings | AxonRouter `embeddings/route.ts` |
| `POST /v1/audio/speech` | OpenAI TTS | Text-to-speech | AxonRouter `ttsCore.tsx` |
| `POST /v1/audio/transcriptions` | OpenAI STT | Speech-to-text | AxonRouter `sttCore.ts` |
| `POST /v1/images/generations` | OpenAI | Image generation | AxonRouter `imageGenerationCore.ts` |
| `POST /v1/video/generations` | OpenAI | Video generation | AxonRouter `video/generations/route.ts` |
| `POST /v1/unified` | Multi | Unified multi-modality gateway | AxonRouter `unified/route.ts` |
| `POST /v1/messages/count_tokens` | Claude | Token counting | AxonRouter `count_tokens/route.ts` |

**Unified endpoint** dispatch ke endpoint yang sesuai berdasarkan `mode` field:
- `text` → `/v1/chat/completions`
- `image` → `/v1/images/generations`
- `audio` → `/v1/audio/speech`
- `video` → `/v1/video/generations`

### 4.2 Providers

Setiap provider punya input mechanism yang berbeda. **Codex ≠ OpenAI** meskipun model name mirip (`cx/gpt-5.4` vs `openai/gpt-5.4`).

#### Provider Input Mechanisms

| Provider | Prefix | Format | Auth Type | Input yang dibutuhkan |
|----------|--------|--------|-----------|---------------------|
| **OpenCode Free** | `oc/` | openai | none | (auto activate) |
| **OpenCode Zen** | `oc-zen/` | openai | API key | Paste key |
| **OpenCode Go** | `oc-go/` | openai | API key | Paste key |
| **MiMoCode (Free)** | `mimocode/` | openai | none (bootstrap JWT) | Auto activate |
| **MiMo PAYG** | `mimo/` | openai | API key | Paste key |
| **MiMo Token Plan** | `mimo-tp/` | openai | API key (`tp-*`) | Paste key |
| **DeepSeek** | `deepseek/` | openai | API key | Paste API key |
| **Claude** | `claude/` | claude | OAuth PKCE | OAuth redirect flow |
| **Gemini** | `gemini/` | gemini | API key | Paste API key |
| **Codex** | `cx/` | openai-responses | OAuth device code | OAuth redirect flow (PKCE) |
| **Antigravity** | `ag/` | antigravity | OAuth Google | OAuth redirect flow (Google) |
| **Kiro** | `kiro/` | kiro | OAuth AWS | OAuth redirect flow (AWS) |
| **OpenAI** | `openai/` | openai | API key | Paste API key |
| **Groq** | `groq/` | openai | API key | Paste API key |
| **ElevenLabs** | `elevenlabs/` | openai | API key | Paste API key |
| **Deepgram** | `deepgram/` | openai | API key | Paste API key |
| **Custom OpenAI** | `<name>/` | openai | API key | Name + URL + API key |
| **Custom Claude** | `<name>/` | claude | API key | Name + URL + API key |

#### Contoh Perbedaan Codex vs OpenAI

```
// Codex — OAuth flow, openai-responses format
cx/gpt-5.4:
  OAuth URL:    https://auth.openai.com/oauth/authorize
  Token URL:    https://auth.openai.com/oauth/token
  Client ID:    app_EMoamEEZ73f0CkXaXp7hrann
  Redirect URI: http://localhost:1455/auth/callback
  Format:       openai-responses
  Base URL:     https://chatgpt.com/backend-api/codex/responses
  Token:        { id_token, access_token, refresh_token, account_id, email }
  Refresh:      OAuth refresh token flow

// OpenAI — API key, openai format
openai/gpt-5.4:
  Format:    openai
  Base URL:  https://api.openai.com/v1/chat/completions
  Auth:      API key (Bearer token)
  Refresh:   ❌ (API key doesn't expire)
```

Ini dua connection berbeda ke dua backend berbeda dengan auth dan format berbeda.

#### Contoh Perbedaan OpenCode (3 Variant)

```
// OpenCode Free — no auth, public endpoint
oc/kimi-k2:
  Format:    openai
  Base URL:  https://opencode.ai/zen/v1/chat/completions
  Auth:      none (public endpoint)
  Models:    Kimi, GLM, Qwen, MiMo, MiniMax (free models)
  Quota:     Rate limited

// OpenCode Zen — API key, same endpoint
oc-zen/kimi-k2:
  Format:    openai
  Base URL:  https://opencode.ai/zen/v1/chat/completions
  Auth:      API key (Bearer token)
  Website:   https://opencode.ai/zen

// OpenCode Go — API key, different endpoint
oc-go/kimi-k2:
  Format:    openai
  Base URL:  https://opencode.ai/zen/go/v1/chat/completions
  Auth:      API key (Bearer token)
  Website:   https://opencode.ai/go
  Note:      Qwen models reject oa-compat format, need special handling
```

Ini tiga provider berbeda: free (no auth), zen (API key), go (API key + different endpoint).

#### Contoh Perbedaan MiMo (3 Variant)

```
// MiMoCode (Free) — bootstrap JWT, no API key
mimocode/mimo-auto:
  Format:    openai
  Base URL:  https://api.xiaomimimo.com/api/free-ai/openai/chat
  Auth:      none (bootstrap JWT, auto-generated)
  Header:    X-Mimo-Source: mimocode-cli-free
  Model:     mimo-auto (auto-select)
  Quota:     Rate limited

// MiMo PAYG — API key, per-usage billing
mimo/mimo-v2.5-pro:
  Format:    openai
  Base URL:  https://api.xiaomimimo.com/v1/chat/completions
  Auth:      API key (Bearer token)
  Models:    mimo-v2.5-pro, mimo-v2.5, mimo-v2-pro, mimo-v2-omni, mimo-v2-flash
  Quota:     Per-usage (PAYG)

// MiMo Token Plan — tp-* key, regional endpoint, monthly quota
mimo-tp/mimo-v2.5-pro:
  Format:    openai
  Base URL:  https://token-plan-sgp.xiaomimimo.com/v1/chat/completions
  Auth:      API key (tp-* prefix, Bearer token)
  Models:    mimo-v2.5-pro, mimo-v2.5, mimo-v2-pro, mimo-v2-omni, mimo-v2-flash
  Quota:     4.1B tokens/month (self-tracked)
  Regional:  token-plan-sgp (SGP), token-plan-cn (CN), token-plan-ams (AMS)
```

Ini tiga provider berbeda: free (bootstrap), paid per-usage, dan paid monthly plan.

#### Contoh Perbedaan Antigravity vs Gemini

```
// Antigravity — Google OAuth, antigravity format
ag/gemini-2.5-pro:
  OAuth:     Google OAuth (load code assist)
  Format:    antigravity
  Base URL:  https://daily-cloudcode-pa.googleapis.com
  UserAgent: antigravity/1.107.0 linux/x64

// Gemini — API key, gemini format
gemini/gemini-2.5-pro:
  Format:    gemini
  Base URL:  https://generativelanguage.googleapis.com/v1beta/models
  Auth:      API key (x-goog-api-key)
```

#### Custom Providers

User bisa tambah provider custom yang compatible dengan OpenAI atau Anthropic API format. Nama yang diberikan user menjadi nama utama provider (bukan nama acak).

**Custom OpenAI-Compatible:**
```
Provider Name: "9router"        ← user-given name, jadi identifier utama
Base URL: https://api.9router.com/v1
Format: openai
Auth: API key (Bearer token)
```

**Custom Anthropic-Compatible:**
```
Provider Name: "my-claude-proxy"  ← user-given name
Base URL: https://my-proxy.example.com
Format: claude
Auth: API key (x-api-key header)
```

**Naming Rules:**
- Nama provider = nama yang user berikan (bukan auto-generated)
- Nama harus unique (tidak boleh sama dengan built-in provider names)
- Nama dipakai sebagai identifier di API: `9router/gpt-4o`, `my-claude-proxy/claude-sonnet-4`
- Nama bisa diubah setelah create
- Format: lowercase, alphanumeric, dash allowed (e.g., `9router`, `my-proxy-v2`)

**Provider Add Flow:**
1. User klik "Add Provider" di dashboard
2. Pilih provider dari list (built-in) atau "Add Custom" (custom)
3. Provider-specific flow:
   - **API key providers** (Mimo, DeepSeek, OpenAI, Groq, dll): Paste API key → validate → save
   - **Free providers** (OpenCode): One-click → auto-activate
n   - **OAuth providers** (Codex, Antigravity, Kiro, Claude): OAuth redirect flow → callback → save tokens
   - **Custom providers**: Input name + format + base URL + API key → test → save
4. Auto-discover available models
5. Test connection: send test request, show success/fail
6. Set capabilities: which modalities this connection supports (TTS, STT, Image, Video)

**Bulk Add (untuk ratusan akun):**
- Bulk paste: paste banyak API key sekaligus (satu per baris)
- Auto-validate semua key
- Auto-discover models
- Show progress bar + summary (X berhasil, Y gagal)

**Provider Add Flow:**
1. User klik "Add Provider" di dashboard
2. Pilih provider dari list
3. Provider-specific flow:
   - **API key providers**: Paste API key → validate (test request) → save to SQLite
   - **Free providers** (OpenCode): One-click → auto-activate
   - **OAuth providers** (Codex, Antigravity, Kiro): Click "Connect" → OAuth redirect → callback → save tokens to SQLite
4. Auto-discover available models
5. Test connection: send test request, show success/fail
6. Set capabilities: which modalities this connection supports (TTS, STT, Image, Video)

**Bulk Add (untuk ratusan akun):**
- Bulk paste: paste banyak API key sekaligus (satu per baris)
- Auto-validate semua key
- Auto-discover models
- Show progress bar + summary (X berhasil, Y gagal)

**Reference files:**
- CLIProxyAPI `internal/auth/codex/` — OAuth device code flow
- CLIProxyAPI `internal/auth/antigravity/` — Google OAuth
- CLIProxyAPI `internal/auth/claude/` — Anthropic OAuth + PKCE
- AxonRouter `src/lib/credentials/` — credential validation

### 4.3 Translation Layer

**Architecture:** Hub-and-spoke via OpenAI format, dengan direct translation untuk known pairs.

```
Client Request (any format) → OpenAI (intermediate) → Target Provider Format
Target Response → OpenAI (intermediate) → Client Format
```

**Translation Pairs:**

| Source → Target | Reference | Complexity |
|----------------|-----------|-----------|
| openai → openai | passthrough | trivial |
| openai → claude | CLIProxyAPI `internal/translator/openai/claude/` | medium |
| openai → gemini | CLIProxyAPI `internal/translator/openai/gemini/` | medium |
| openai → codex (responses) | CLIProxyAPI `internal/translator/codex/openai/responses/` | medium |
| openai → antigravity | CLIProxyAPI `internal/translator/antigravity/openai/` | high |
| openai → kiro | AxonRouter `translator/request/openai-to-kiro.ts` | high |
| claude → openai | CLIProxyAPI `internal/translator/claude/openai/` | medium |
| gemini → openai | CLIProxyAPI `internal/translator/gemini/openai/` | medium |
| codex → openai | CLIProxyAPI `internal/translator/codex/openai/` | medium |
| antigravity → openai | CLIProxyAPI `internal/translator/antigravity/openai/` | high |
| kiro → openai | AxonRouter `translator/response/kiro-to-openai.tsx` | high |

**Format identifiers:**
```
openai, openai-responses, claude, gemini, gemini-cli, vertex,
antigravity, kiro, cursor, ollama, commandcode
```

**Reference files:**
- CLIProxyAPI `internal/translator/` — per-provider translator structure (Go)
- CLIProxyAPI `internal/translator/translator/translator.go` — translator registry
- AxonRouter `open-sse/translator/index.ts` — hub-and-spoke translateRequest/translateResponse
- AxonRouter `open-sse/translator/formats.ts` — format identifiers

### 4.4 Combo System

**Combo** = named ordered list of model steps dengan routing strategy.

```
Combo "balanced":
  1. mimo/mimo-v2-pro  (priority 1)
  2. codex/gpt-5       (priority 2)
  3. opencode/gpt-4o   (priority 3, free fallback)
```

**Smart Combo** = combo dengan goal-based resolution (auto-resolve):
- `auto` — dynamic: analyze telemetry → choose goal → select combo
- `economy` — cheapest combo
- `balanced` — default balanced
- `premium` — highest quality

**Features:**
- Create/edit/delete combos via dashboard
- Add model steps (provider/model format)
- Strategy: `priority` (try in order) or `round-robin`
- Circuit breaker per model: 3 failures → OPEN → 60s → HALF_OPEN → 2 successes → CLOSED
- Fallback: if model fails, try next in list
- Timeout budget per combo (default 30s)
- Rotation state persisted di SQLite
- Smart combo resolution → combo selection based on goal

**Reference files:**
- AxonRouter `open-sse/services/combo.tsx` — main combo handler
- AxonRouter `src/lib/routing/virtualModelResolver.ts` — smart combo (virtual models)
- AxonRouter `src/lib/routing/defaultCombos.ts` — default combo generation
- AxonRouter `src/lib/routing/fallbackGraph.ts` — fallback graph
- AxonRouter `open-sse/services/autoCombo/scoring.ts` — scoring algorithm
- AxonRouter `open-sse/services/autoCombo/taskFitness.ts` — task fitness
- AxonRouter `open-sse/services/autoCombo/modePacks.ts` — mode packs

### 4.5 Account State Detection & Management

Sistem mendeteksi kondisi akun secara otomatis dari response error dan mengubah connection status agar akun yang bermasalah di-skip dari routing tanpa menambah latency.

**Design Constraint:** Ratusan hingga ribuan akun per provider. Routing harus <1ms regardless of connection count.

#### Connection Status (Final Model)

```go
type ConnectionStatus string

const (
    StatusReady           ConnectionStatus = "ready"            // siap dipakai
    StatusRateLimited     ConnectionStatus = "rate_limited"     // TPM/RPM limit (per model)
    StatusQuotaExhausted  ConnectionStatus = "quota_exhausted"  // weekly/monthly quota
    StatusBalanceEmpty    ConnectionStatus = "balance_empty"    // saldo 0, manual top up
    StatusAuthFailed      ConnectionStatus = "auth_failed"      // API key invalid / token expired
    StatusSuspended       ConnectionStatus = "suspended"        // akun di-ban provider
    StatusDisabled        ConnectionStatus = "disabled"         // user disable manual
)
```

#### Detection Rules

| Kondisi | Trigger | HTTP Status | Error Patterns | Status | Scope | Cooldown | Auto-Recover |
|---------|---------|------------|----------------|--------|-------|----------|--------------|
| TPM/RPM limit | auto | 429 | — | `rate_limited` | per model | Retry-After (1-2min) | ✅ Ya |
| Quota exhausted | auto | 402, 429 | "quota exceeded", "weekly quota exhausted" | `quota_exhausted` | per connection | 5 jam - 1 hari (dari response) | ✅ Ya (kalau tau reset time) |
| Balance empty | auto | 402 | "insufficient quota", "billing hard limit", "add credits" | `balance_empty` | per connection | ❌ | ❌ Manual (top up) |
| API key invalid | auto | 401 | "invalid api key", "authentication failed" | `auth_failed` | per connection | ❌ | ❌ Manual (update key) |
| Token expired | auto | 401 | "token expired", "unauthorized" | auto-refresh → `ready` atau `auth_failed` | per connection | Auto-refresh | ✅ Ya (kalau refresh berhasil) |
| Akun suspended | auto | 403 | "account suspended", "account disabled" | `suspended` | per connection | ❌ | ❌ Manual |
| Transient error | auto | 502, 503, 504 | — | circuit breaker | per connection | 60s (3 failures) | ✅ Ya |
| Server error | auto | 500 | — | circuit breaker | per connection | 60s (3 failures) | ✅ Ya |
| User disable | manual | — | — | `disabled` | per connection | ❌ | ❌ Manual enable |

#### Provider Quota Capabilities (Verified from Codebase)

**Source:** OmniRoute `src/lib/usage/fetcher.ts` — `getUsageForProvider()`, AxonRouter `src/lib/usageStatus.ts`

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

**Keterangan:**
- **Proactive Quota API** = provider punya endpoint untuk cek quota/usage secara periodik
- **Reactive Detection** = detect dari error response saat request gagal
- **Reset Time** = tau kapan quota akan reset (untuk auto-recover)
- **❌** = tidak ada, harus detect dari error response saja

**Quota Check Strategy:**
- Provider DENGAN quota API: background scheduler cek setiap 30 menit (default, configurable), update state
- Provider TANPA quota API: hanya reactive detection dari error response (429, 402)
- Semua provider: reactive detection dari error text patterns (AxonRouter `usageStatus.ts`)

#### 3-Layer Defense Architecture

```
Layer 1: CONNECTION STATE (in-memory sync.Map) ← Routing cek ini, <1ms
Layer 2: CIRCUIT BREAKER (per-connection) ← CLOSED → OPEN → HALF_OPEN
Layer 3: MODEL RATE LIMIT (per model per connection) ← TPM/RPM tracking
```

#### Routing Fast Path (<1ms)

```go
// Pre-computed eligible list, updated async saat state berubah
type ProviderRouter struct {
    // Snapshot of eligible connections, sorted by priority/latency
    eligibleSnapshot atomic.Value  // []*Connection
    
    // All connections (for state updates)
    connections sync.Map  // connectionID → *ConnectionState
}

// Routing: O(1) — tinggal pick dari pre-computed list
func (r *ProviderRouter) PickConnection(modelID string) *Connection {
    eligible := r.eligibleSnapshot.Load().([]*Connection)
    
    for _, conn := range eligible {
        // Check model-level rate limit
        if conn.ModelLimits[modelID].IsCooldown() {
            continue
        }
        return conn
    }
    
    // Fallback: semua rate-limited, ambil yang paling cepat recover
    return r.getEarliestRecoverable(eligible, modelID)
}

// State update: async, trigger re-compute eligible list
func (r *ProviderRouter) UpdateState(connID string, status ConnectionStatus) {
    r.connections.Store(connID, status)
    r.recomputeEligible()  // async, non-blocking
}
```

#### 2-Level Rate Limit

```
Connection Level:
  ready ← normal
  quota_exhausted ← weekly/monthly quota (auto-recover setelah reset)
  balance_empty ← saldo 0 (manual top up)
  auth_failed ← API key invalid (manual update)
  suspended ← akun di-ban (manual)
  disabled ← user disable (manual)

Model Level (per model per connection):
  normal ← TPM/RPM masih cukup
  rate_limited ← TPM/RPM habis (hanya model ini, model lain masih jalan)
  near_limit ← TPM sisa <10% (prefer connection lain kalau ada)
```

#### Token Refresh Flow

- OAuth providers (Codex, Antigravity, Kiro) punya refresh token
- Saat 401 detected → auto-refresh token
- Jika refresh berhasil → request retry dengan token baru
- Jika refresh gagal → set `auth_failed`, user harus re-authorize via dashboard

#### Response Headers to Parse

```go
// OpenAI-style
"x-ratelimit-remaining-requests"  → RPM remaining
"x-ratelimit-remaining-tokens"    → TPM remaining
"retry-after"                     → cooldown seconds

// Claude-style
"anthropic-ratelimit-requests-remaining" → RPM remaining
"anthropic-ratelimit-tokens-remaining"   → TPM remaining
```

**Reference files:**
- AxonRouter `src/lib/usageStatus.ts` — error pattern matching, status sync
- AxonRouter `src/lib/connectionStatus.ts` — status fields
- AxonRouter `src/lib/providerCooldown.ts` — cooldown calculation
- AxonRouter `src/lib/providerEligibility.ts` — eligibility snapshot
- AxonRouter `src/lib/providerHotState.ts` — hot state (in-memory + SQLite WAL)
- AxonRouter `open-sse/services/accountFallback.ts` — fallback logic, exponential backoff
- AxonRouter `open-sse/services/circuitBreaker.ts` — circuit breaker per connection

### 4.6 Dashboard

**Scale constraint:** Ratusan hingga ribuan connections per provider. Semua list page harus pagination.

| Page | Features | Scale Considerations |
|------|----------|---------------------|
| **Home** | Overview: total connections per status, per provider summary, active combos | Aggregated counts, bukan list |
| **Providers** | Card per provider: connection count per status, quick actions | Show counts, expand to see connections |
| **Provider Detail** | Connection list with pagination, search, filter by status | Pagination 50/page, search by name/key, filter by status |
| **Connection Detail** | Status, models, usage, last error, cooldown timer | Single connection view |
| **Combos** | List combos, create/edit/delete, model steps | Normal list |
| **Logs** | Request history with pagination, filters | Pagination 100/page, filter by provider/model/status/date |
| **Settings** | API keys, rate limits, concurrency | Normal form |

**Provider Page UX (1000+ connections):**
```
┌─────────────────────────────────────────────────────────┐
│  Providers                                    [+ Add]   │
├─────────────────────────────────────────────────────────┤
│                                                         │
│  ┌─────────────────────────────────────────────────┐   │
│  │ 🟢 OpenAI                          847 accounts  │   │
│  │    🟢 Ready: 812  🟡 Rate Limited: 23           │   │
│  │    🔴 Balance Empty: 8  ⚫ Disabled: 4          │   │
│  │    [View All] [Bulk Add] [Test All]              │   │
│  └─────────────────────────────────────────────────┘   │
│                                                         │
│  ┌─────────────────────────────────────────────────┐   │
│  │ 🟢 Codex                           234 accounts  │   │
│  │    🟢 Ready: 198  🟡 Quota Exhausted: 31        │   │
│  │    🔴 Auth Failed: 5                            │   │
│  │    [View All] [Bulk Add] [Test All]              │   │
│  └─────────────────────────────────────────────────┘   │
│                                                         │
│  ┌─────────────────────────────────────────────────┐   │
│  │ 🟢 Mimo                            500 accounts  │   │
│  │    🟢 Ready: 489  🔴 Balance Empty: 11          │   │
│  │    [View All] [Bulk Add] [Test All]              │   │
│  └─────────────────────────────────────────────────┘   │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

**Connection List UX (1000+ connections in provider):**
```
┌──────────────────────────────────────────────────────────────────┐
│  OpenAI Connections                                    [Bulk Add] │
│                                                                   │
│  Filter: [All Status ▼]  Search: [________________]               │
│                                                                   │
│  Showing 1-50 of 847 connections                                  │
│                                                                   │
│  ┌─────┬──────────────────┬─────────┬─────────┬──────┬────────┐ │
│  │ #   │ Name             │ Status  │ Models  │ Req  │ Action │ │
│  ├─────┼──────────────────┼─────────┼─────────┼──────┼────────┤ │
│  │ 1   │ openai-key-001   │ 🟢 Ready│ 3       │ 1.2k │ [Edit] │ │
│  │ 2   │ openai-key-002   │ 🟢 Ready│ 3       │ 890  │ [Edit] │ │
│  │ 3   │ openai-key-003   │ 🟡 Rate │ 2       │ 2.1k │ [Edit] │ │
│  │ ... │ ...              │ ...     │ ...     │ ...  │ ...    │ │
│  │ 50  │ openai-key-050   │ 🟢 Ready│ 3       │ 456  │ [Edit] │ │
│  └─────┴──────────────────┴─────────┴─────────┴──────┴────────┘ │
│                                                                   │
│  [← Prev]  Page 1/17  [Next →]                                   │
│                                                                   │
│  Bulk Actions: [Select All] [Disable Selected] [Test Selected]    │
│                                                                   │
└──────────────────────────────────────────────────────────────────┘
```

**Connection Detail UX:**
```
┌─────────────────────────────────────────────────────────┐
│  Connection: openai-key-001                    [Delete]  │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  Status: 🟢 Ready                                       │
│  Provider: OpenAI                                        │
│  Auth: API Key (sk-...abc123)                            │
│  Created: 2026-06-15 10:30                               │
│  Last Used: 2026-06-27 14:23                             │
│                                                          │
│  Models:                                                 │
│    gpt-4o       🟢 Ready     TPM: 8,500/10,000          │
│    gpt-4o-mini  🟢 Ready     TPM: 95,000/100,000        │
│    o3           🟡 Rate Limited (reset in 45s)           │
│                                                          │
│  Usage (Today):                                          │
│    Requests: 1,234 | Tokens: 456,789 | Cost: $12.34     │
│    Success Rate: 99.2% | Avg Latency: 1.2s               │
│                                                          │
│  Last Error:                                             │
│    [2026-06-27 14:20] 429 Rate Limit Exceeded            │
│                                                          │
│  Actions: [Test Connection] [Disable] [Reset Status]     │
│                                                          │
└─────────────────────────────────────────────────────────┘
```

**Reference files:**
- AxonRouter `src/app/(dashboard)/app/` — dashboard pages
- AxonRouter `src/app/(dashboard)/app/providers/` — provider management UI

### 4.7 Quota & Usage Tracking

**Per request log:**
- Timestamp, connection_id, provider_type, model_id, combo_name
- Input/output/reasoning tokens
- Latency (ms), status code, error message
- Modality (chat, tts, stt, image, video)
- Cost (estimated from model pricing)

**Per connection:**
- Total requests, total tokens
- Success/failure rate
- Last used timestamp
- Rate limit state (TPM/RPM remaining per model)
- Quota state (quota_exhausted/balance_empty/ready)

**Dashboard views:**
- Per-provider usage chart (requests/day, tokens/day)
- Per-model cost breakdown
- Top connections by usage
- Error rate timeline
- Quota remaining per provider (if provider reports)

**Async logging:**
- Request logs di-buffer di memory, flush ke SQLite setiap 5 detik atau 100 entries
- Tidak blocking request path

**Reference files:**
- AxonRouter `src/lib/usageDb/` — usage tracking
- AxonRouter `src/lib/rateLimiter.ts` — rate limiting
- AxonRouter `src/lib/chat/concurrencyLimiter.ts` — concurrency limiter

### 4.8 Auth

**Client → Proxy:**
- API key: `Authorization: Bearer <proxy-api-key>`
- Multiple keys with different rate limits
- Optional: disable for local dev

**Proxy → Connection:**
- API key: stored in SQLite
- OAuth: token refresh flow, stored in SQLite
- Free: no auth

**Dashboard:**
- Password login (single admin)
- Session cookie

**Reference files:**
- CLIProxyAPI `internal/auth/` — OAuth flows
- AxonRouter `src/lib/auth/` — management auth

### 4.9 CLI (Minimal)

```
axonrouter                    # Interactive menu (default)
axonrouter run                # Direct run (foreground, auto-kill, stream logs)
axonrouter run --port 8080    # Direct run on custom port
axonrouter run --no-kill      # Direct run, fail if port in use
axonrouter status             # Show service status (non-interactive)
axonrouter stop               # Stop service
axonrouter restart            # Restart service
axonrouter version            # Show version
axonrouter help               # Show help
```

---

## 5. Non-Functional Requirements

| Requirement | Target |
|------------|--------|
| **Concurrent streams** | 1000+ SSE connections |
| **Proxy overhead** | <5ms |
| **Routing latency** | <1ms (regardless of connection count) |
| **Error handling** | Graceful di semua path |
| **Memory usage** | <100MB idle, <500MB dengan 5000 connections |
| **Binary size** | <50MB |
| **Startup time** | <1s |
| **Test coverage** | >80% translator + combo |
| **Zero external deps** | SQLite embedded, no Redis/Postgres |

---

## 6. Out of Scope

- Multi-user / team management
- Plugin system
- WebSocket relay
- TUI (Charmbracelet)
- File-based config (YAML)
- External DB (Postgres/Redis)
- Morph provider (special integration)
