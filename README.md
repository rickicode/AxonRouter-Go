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

### Install via installer (no build needed)

The `installer.sh` script auto-detects your OS/architecture, downloads the
matching binary from the latest GitHub Release, and installs it onto your PATH:

```bash
# One-liner (downloads + runs the installer)
curl -fsSL https://raw.githubusercontent.com/rickicode/AxonRouter-Go/main/installer.sh | bash

# Or clone first and run locally
./installer.sh                 # latest release, auto OS/arch detection
./installer.sh --version v1.2.3 # pin a specific tag
./installer.sh --to /usr/local/bin  # change install directory
```

Supported targets: **windows/amd64**, **linux/amd64**, **darwin/amd64**,
**darwin/arm64**. The script fetches the asset named
`axonrouter-<os>-<arch>[.exe]` that the release workflow publishes.

> Requires `curl` on the host. On Windows, run it from Git Bash / WSL.

### Releases (GitHub Actions)

Pushing a tag matching `v*` (e.g. `v1.2.3`) triggers
[`.github/workflows/release.yml`](.github/workflows/release.yml), which builds
a cross-platform matrix (Windows / Linux / macOS) and attaches the binaries to
the GitHub Release. Create the release from the **Releases** page (Draft a new
release, tag it `vX.Y.Z`) and the workflow uploads the assets automatically —
`installer.sh` then pulls the right one for each user.

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
| `POST /v1/messages/count_tokens` | Claude | Token counting |
| `GET /v1/models` | — | Model listing + combos + virtual models |
| `POST /v1/audio/speech` | OpenAI TTS | Text-to-speech |
| `POST /v1/audio/transcriptions` | OpenAI STT | Speech-to-text |
| `POST /v1/images/generations` | OpenAI | Image generation |
| `POST /v1/video/generations` | OpenAI | Video generation |
| `POST /v1/unified` | Multi | Unified multi-modality gateway |

> **Note:** Anthropic clients that append an extra `/v1` segment to the base URL
> are handled via the `/v1/v1/messages` alias. The Codex Responses API is reached
> through the OpenAI Chat `/v1/chat/completions` path (translated to
> `openai-responses` format internally) — there is no standalone `/v1/responses`
> route.

### Admin API

Authentication: JWT session issued by `POST /api/admin/login`. All `/api/admin/*`
routes (except `login` and `health`) require the session cookie.

**Auth / Health**

| Endpoint | Description |
|----------|-------------|
| `POST /api/admin/login` | Issue a session JWT |
| `GET /api/admin/health` | Health check (no auth) |
| `GET /api/admin/metrics` | Prometheus-style metrics |

**Providers**

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
| `POST /api/admin/providers/validate` | Validate an API key |

**Connections**

| Endpoint | Description |
|----------|-------------|
| `GET /api/admin/providers/:id/connections` | List connections |
| `GET /api/admin/connections/:id` | Connection detail |
| `PATCH /api/admin/connections/:id` | Update connection |
| `DELETE /api/admin/connections/:id` | Delete connection |
| `POST /api/admin/connections/:id/test` | Test single connection |
| `POST /api/admin/connections/:id/refresh` | Refresh OAuth token |
| `POST /api/admin/connections/:id/reset` | Reset connection status |
| `PATCH /api/admin/connections/bulk` | Bulk update connections |

**Models**

| Endpoint | Description |
|----------|-------------|
| `GET /api/admin/providers/:id/models` | List models for a provider |
| `POST /api/admin/providers/:id/models/test` | Test a model |
| `POST /api/admin/models/sync` | Sync model catalog |

**OAuth**

| Endpoint | Description |
|----------|-------------|
| `POST /api/admin/oauth/start` | Start an OAuth flow |
| `GET /api/admin/oauth/:sessionId/poll` | Poll OAuth status |
| `POST /api/admin/oauth/callback` | Submit OAuth callback |

**Combos**

| Endpoint | Description |
|----------|-------------|
| `GET /api/admin/combos` | List combos |
| `GET /api/admin/combos/:id` | Combo detail |
| `POST /api/admin/combos` | Create combo |
| `PATCH /api/admin/combos/:id` | Update combo |
| `DELETE /api/admin/combos/:id` | Delete combo |
| `POST /api/admin/combos/:id/steps` | Add combo step |
| `DELETE /api/admin/combos/steps/:stepId` | Remove combo step |
| `POST /api/admin/combos/seed-defaults` | Seed default combos |

**Logs**

| Endpoint | Description |
|----------|-------------|
| `GET /api/admin/logs` | Request logs (paginated) |
| `GET /api/admin/logs/stats` | Log statistics |
| `GET /api/admin/logs/active` | Active requests |

**Settings**

| Endpoint | Description |
|----------|-------------|
| `GET /api/admin/settings` | List settings |
| `GET /api/admin/settings/:key` | Get setting |
| `PUT /api/admin/settings/:key` | Update setting |
| `DELETE /api/admin/settings/:key` | Delete setting |

**Dashboard**

| Endpoint | Description |
|----------|-------------|
| `GET /api/admin/dashboard/stats` | Dashboard statistics |
| `GET /api/admin/dashboard/providers` | Provider summary |
| `GET /api/admin/dashboard/recent-logs` | Recent logs |

**Quota**

| Endpoint | Description |
|----------|-------------|
| `GET /api/admin/quota` | List quota cache |
| `GET /api/admin/quota/summary` | Quota summary |
| `POST /api/admin/quota/:connId/refresh` | Refresh a connection's quota |

**Model Pricing**

| Endpoint | Description |
|----------|-------------|
| `GET /api/admin/model-pricing` | List per-model cost rates |
| `POST /api/admin/model-pricing` | Create pricing entry |
| `PATCH /api/admin/model-pricing/:id` | Update pricing entry |
| `DELETE /api/admin/model-pricing/:id` | Delete pricing entry |

**Proxy Pools**

| Endpoint | Description |
|----------|-------------|
| `GET /api/admin/proxy-pools` | List proxy pools |
| `POST /api/admin/proxy-pools` | Create proxy pool |
| `POST /api/admin/proxy-pools/bulk` | Bulk create proxy pools |
| `GET /api/admin/proxy-pools/:id` | Proxy pool detail |
| `PATCH /api/admin/proxy-pools/:id` | Update proxy pool |
| `DELETE /api/admin/proxy-pools/:id` | Delete proxy pool |
| `POST /api/admin/proxy-pools/:id/test` | Test proxy pool |
| `GET /api/admin/proxy-pools/health-check` | Get health-check status |
| `POST /api/admin/proxy-pools/health-check` | Run health check |
| `GET /api/admin/proxy-pools/generate-source` | Generate deploy source |
| `POST /api/admin/proxy-pools/vercel-deploy` | Deploy to Vercel |
| `POST /api/admin/proxy-pools/deno-deploy` | Deploy to Deno |
| `POST /api/admin/proxy-pools/cloudflare-deploy` | Deploy to Cloudflare |

## Providers

| Provider | Prefix | Format | Auth |
|----------|--------|--------|------|
| OpenAI | `openai/` | openai | API key |
| Claude | `claude/` | anthropic | OAuth PKCE |
| Gemini | `gemini/` | gemini | API key |
| Codex | `cx/` | openai-responses | OAuth device code |
| Antigravity | `ag/` | antigravity | OAuth Google |
| Kiro | `kiro/` | kiro (openai-compatible) | OAuth AWS |
| Z.ai | `zai/` | claude | API key |
| DeepSeek | `deepseek/` | openai | API key |
| Groq | `groq/` | openai | API key |
| MiMoCode | `mimocode/` | openai | none (free) |
| MiMoCode Free | `mimocode-free/` | openai | none (free) |
| MiMo Token Plan | `mimo-tp/` | openai | API key |
| OpenRouter | `openrouter/` | openai | API key |
| OpenCode | `oc/` | openai | none (free) |
| OpenCode Zen | `oc-zen/` | openai | API key |
| OpenCode Go | `oc-go/` | openai | API key |
| Cloudflare Workers AI | `cf/` | openai | API key |
| ElevenLabs | `elevenlabs/` | openai | API key |
| Deepgram | `deepgram/` | openai | API key |
| Custom OpenAI | `<name>/` | openai | API key |
| Custom Claude | `<name>/` | claude | API key |

## Translation Pairs

```
openai ↔ claude
openai ↔ gemini
openai ↔ codex-responses
openai ↔ antigravity
openai ↔ kiro
openai ↔ openai_responses (passthrough)
claude ↔ antigravity
claude ↔ gemini
codex  ↔ claude
codex  ↔ gemini
antigravity ↔ gemini
gemini ↔ claude
openai ↔ openai (passthrough)
```

Total: **18 registered pairs** (hub-and-spoke via OpenAI + direct translation for known pairs + passthrough)

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
make build          # Build frontend + backend (production binary)
make frontend       # Build frontend only
make backend        # Build backend only
make build-dev      # Build a separate axonrouter-dev binary (never clobbers live)
make run            # Build and run on port 3777
make run-dev        # Build + run dev server on port 3788 (isolated data dir)
make dev            # Start frontend dev server (hot reload, port 5173)
make install        # Install frontend dependencies
make clean          # Clean build artifacts (incl. DB/session files)
make kill-port      # Kill process listening on the live port
make test           # Run tests
make lint           # Run linter
```

## License

MIT License
