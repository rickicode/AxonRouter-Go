# AxonRouter-Go

Universal API proxy for coding agents (Claude Code, Codex CLI, Cursor, Kiro, dll). Single Go binary, single port, embedded Svelte dashboard, SQLite storage, zero external dependencies.

## Features

- **Universal Proxy** — Translate antara OpenAI, Claude, Gemini, Codex, Antigravity, Kiro formats
- **11 Translation Pairs** — Hub-and-spoke via OpenAI format + direct translation untuk known pairs
- **Connection Management** — Manage 100-1000+ API keys per provider
- **Combo Routing** — Smart fallback routing dengan circuit breaker (3 failures → OPEN → 60s → HALF_OPEN → 2 successes → CLOSED)
- **O(1) Routing** — Eligibility snapshot, <1ms regardless of connection count
- **OAuth Support** — Auto-refresh untuk Codex, Antigravity, Kiro
- **Rate Limiting** — Per-key rate limiting + rate limit header parsing (OpenAI & Claude style)
- **Error Detection** — Auto-detect rate_limit, quota_exhausted, balance_empty, auth_failed dari response
- **Dashboard** — Web-based management UI (Svelte + Tailwind)
- **Zero Dependencies** — Single binary dengan embedded SQLite dan frontend

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Backend | Go + Gin + SQLite (WAL mode) |
| Frontend | SvelteKit + Vite + Tailwind CSS |
| Database | SQLite (embedded, zero config) |
| Build | Static frontend embedded via `go:embed` |

## Quick Start

### Prerequisites

- Go 1.22+
- Node.js 18+ (untuk frontend build)

### Installation

```bash
git clone https://github.com/rickicode/AxonRouter-Go.git
cd AxonRouter-Go

# Install frontend dependencies
cd web && npm install && cd ..

# Build everything
make build

# Run
./build/axonrouter
```

Server starts di port **3777**. Dashboard: http://localhost:3777

### Development

```bash
# Frontend hot reload (port 5173)
make dev

# Build frontend only
make frontend

# Build backend only
make backend

# Full build
make build
```

## API Endpoints

### Proxy Endpoints

| Endpoint | Format | Description |
|----------|--------|-------------|
| `POST /v1/chat/completions` | OpenAI Chat | Chat completion (main) |
| `POST /v1/messages` | Claude | Anthropic Messages API |
| `POST /v1/responses` | OpenAI Responses | Codex Responses API |
| `GET /v1/models` | — | Model listing + combos + virtual models |
| `POST /v1/embeddings` | OpenAI | Embeddings |
| `POST /v1/audio/speech` | OpenAI TTS | Text-to-speech |
| `POST /v1/audio/transcriptions` | OpenAI STT | Speech-to-text |
| `POST /v1/images/generations` | OpenAI | Image generation |
| `POST /v1/video/generations` | OpenAI | Video generation |
| `POST /v1/unified` | Multi | Unified multi-modality gateway |
| `POST /v1/messages/count_tokens` | Claude | Token counting |

### Admin API

| Endpoint | Description |
|----------|-------------|
| `GET /api/admin/providers` | List providers dengan connection counts |
| `GET /api/admin/providers/:id` | Provider detail + status breakdown |
| `POST /api/admin/providers` | Add provider |
| `PATCH /api/admin/providers/:id` | Update provider |
| `DELETE /api/admin/providers/:id` | Delete provider |
| `POST /api/admin/providers/:id/test` | Test all connections |
| `POST /api/admin/providers/:id/connections` | Add connection |
| `POST /api/admin/providers/:id/connections/bulk` | Bulk add connections |
| `GET /api/admin/connections/:id` | Connection detail |
| `PATCH /api/admin/connections/:id` | Update connection |
| `DELETE /api/admin/connections/:id` | Delete connection |
| `POST /api/admin/connections/:id/test` | Test single connection |
| `POST /api/admin/connections/:id/reset` | Reset connection status |
| `GET /api/admin/combos` | List combos |
| `POST /api/admin/combos` | Create combo |
| `PATCH /api/admin/combos/:id` | Update combo |
| `DELETE /api/admin/combos/:id` | Delete combo |
| `GET /api/admin/logs` | Request logs (paginated) |
| `GET /api/admin/settings` | Settings |
| `PUT /api/admin/settings/:key` | Update setting |
| `GET /api/admin/dashboard/stats` | Dashboard statistics |

## Providers

| Provider | Prefix | Format | Auth |
|----------|--------|--------|------|
| OpenAI | `openai/` | openai | API key |
| Claude | `claude/` | claude | OAuth PKCE |
| Gemini | `gemini/` | gemini | API key |
| Codex | `cx/` | openai-responses | OAuth device code |
| Antigravity | `ag/` | antigravity | OAuth Google |
| Kiro | `kiro/` | kiro | OAuth AWS |
| DeepSeek | `deepseek/` | openai | API key |
| Groq | `groq/` | openai | API key |
| MiMo | `mimo/` | openai | API key |
| MiMoCode | `mimocode/` | openai | none (free) |
| MiMo Token Plan | `mimo-tp/` | openai | API key |
| OpenCode | `oc/` | openai | none (free) |
| OpenCode Zen | `oc-zen/` | openai | API key |
| OpenCode Go | `oc-go/` | openai | API key |
| Custom OpenAI | `<name>/` | openai | API key |
| Custom Claude | `<name>/` | claude | API key |

## Translation Pairs

```
openai ↔ claude
openai ↔ gemini
openai ↔ codex-responses
openai ↔ antigravity
openai ↔ kiro
openai ↔ openai (passthrough)
```

Total: **11 registered pairs** (6 forward + 5 reverse)

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│              SINGLE BINARY (port 3777)                    │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │                    Gin Router                      │ │
│  │  /v1/* routes              /api/admin/* routes     │ │
│  │  ┌──────────────────┐     ┌──────────────────────┐ │ │
│  │  │ Proxy Handlers   │     │ Admin Handlers       │ │ │
│  │  │ - chat           │     │ - providers CRUD     │ │ │
│  │  │ - messages       │     │ - connections CRUD   │ │ │
│  │  │ - responses      │     │ - combos CRUD        │ │ │
│  │  │ - models         │     │ - logs (paginated)   │ │ │
│  │  │ - embeddings     │     │ - settings           │ │ │
│  │  │ - audio/tts/stt  │     │ - dashboard stats    │ │ │
│  │  │ - images/video   │     │                      │ │ │
│  │  │ - unified        │     │ Dashboard UI (Svelte)│ │ │
│  │  └──────────────────┘     │ via go:embed         │ │ │
│  │                           └──────────────────────┘ │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │              Shared Internal Packages               │ │
│  │  translator │ auth │ executor │ connstate │ combo   │ │
│  │  usage │ config │ db                               │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │              Background Goroutines                  │ │
│  │  - Quota scheduler (30 min, configurable)          │ │
│  │  - Usage log flush (every 5 sec)                   │ │
│  │  - Circuit breaker cleanup (every 5 min)           │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │              SQLite (WAL mode)                      │ │
│  └────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
```

## Project Structure

```
AxonRouter-Go/
├── cmd/
│   ├── server/              ← Server entry point
│   └── cli/                 ← CLI entry point
├── internal/
│   ├── api/
│   │   ├── handlers/
│   │   │   ├── v1/          ← Proxy handlers (chat, messages, responses, models)
│   │   │   └── admin/       ← Admin handlers (providers, connections, combos, logs)
│   │   ├── middleware/       ← Auth, rate limiting, CORS, logging
│   │   └── router.go        ← Route registration
│   ├── translator/          ← Format translation (11 pairs)
│   │   ├── openai/          ← OpenAI → provider translators
│   │   ├── claude/          ← Claude → OpenAI translator
│   │   ├── gemini/          ← Gemini → OpenAI translator
│   │   ├── codex/           ← Codex → OpenAI translator
│   │   ├── antigravity/     ← Antigravity → OpenAI translator
│   │   ├── kiro/            ← Kiro → OpenAI translator
│   │   ├── registry/        ← Translator registry
│   │   └── types/           ← Format types
│   ├── auth/                ← OAuth flows (Codex, Antigravity, Kiro)
│   ├── executor/            ← Provider executors (OpenAI, Claude, Gemini, etc.)
│   ├── connstate/           ← Connection state, circuit breaker, eligibility
│   ├── combo/               ← Combo routing, smart combos
│   ├── usage/               ← Usage tracking
│   ├── db/                  ← SQLite database, migrations
│   ├── background/          ← Background goroutines
│   └── web/                 ← Embedded frontend
├── web/                     ← Frontend source (SvelteKit)
│   ├── src/
│   │   ├── routes/          ← SvelteKit pages
│   │   └── lib/             ← Shared components
│   ├── build/               ← Static output (embedded)
│   └── package.json
├── docs/
│ ├── TDD.md ← Technical Design Document
│ └── DESIGN.md ← Design System
├── Makefile
├── go.mod
└── README.md
```

## Connection State & Error Detection

### Connection Status

| Status | Trigger | Auto-Recover |
|--------|---------|--------------|
| `ready` | Normal | — |
| `rate_limited` | 429 + rate limit headers | ✅ (Retry-After) |
| `quota_exhausted` | 402 + quota patterns | ✅ (after reset) |
| `balance_empty` | 402 + billing patterns | ❌ (manual top up) |
| `auth_failed` | 401 + auth patterns | ❌ (manual update) |
| `suspended` | 403 + suspend patterns | ❌ (manual) |
| `disabled` | User disable | ❌ (manual enable) |

### Circuit Breaker

```
3 failures → OPEN (stop sending requests)
60 seconds → HALF_OPEN (try 1 request)
2 successes → CLOSED (back to normal)
```

### Rate Limit Header Parsing

```go
// OpenAI-style
"x-ratelimit-remaining-requests"  → RPM remaining
"x-ratelimit-remaining-tokens"    → TPM remaining
"retry-after"                     → cooldown seconds

// Claude-style
"anthropic-ratelimit-requests-remaining" → RPM remaining
"anthropic-ratelimit-tokens-remaining"   → TPM remaining
```

## Configuration

Configuration stored di SQLite `settings` table:

| Key | Default | Description |
|-----|---------|-------------|
| `quota_check_interval` | 30m | Background quota check interval |
| `usage_flush_interval` | 5s | Usage log flush interval |
| `circuit_breaker_cleanup_interval` | 5m | Circuit breaker cleanup |

## Building

```bash
# Full build (frontend + backend)
make build

# Frontend only
make frontend

# Backend only
make backend

# Clean build artifacts
make clean
```

## Makefile Targets

```bash
make build        # Build frontend + backend
make frontend     # Build frontend only
make backend      # Build backend only
make run          # Build and run
make dev          # Start frontend dev server (hot reload)
make install      # Install frontend dependencies
make clean        # Clean build artifacts
make test         # Run tests
make lint         # Run linter
```

## License

MIT License
