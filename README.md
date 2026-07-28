# AxonRouter-Go

<p align="center">
  <a href="https://github.com/rickicode/AxonRouter-Go/releases/latest">
    <img src="https://img.shields.io/github/v/release/rickicode/AxonRouter-Go?style=flat-square&color=ec4899" alt="Latest Release">
  </a>
  <a href="https://github.com/rickicode/AxonRouter-Go/actions/workflows/release.yml">
    <img src="https://img.shields.io/github/actions/workflow/status/rickicode/AxonRouter-Go/release.yml?style=flat-square&label=release%20build" alt="Release Build">
  </a>
  <img src="https://img.shields.io/badge/Go-1.26%2B-blue?style=flat-square" alt="Go 1.26+">
  <img src="https://img.shields.io/badge/Svelte-5%2B-ff3e00?style=flat-square" alt="Svelte 5+">
  <img src="https://img.shields.io/badge/license-MIT-green?style=flat-square" alt="MIT License">
</p>

<p align="center">
  <strong>Universal API proxy for coding agents.</strong><br>
  One Go binary · embedded Svelte dashboard · SQLite · OpenAI / Claude / Gemini / Codex / Antigravity / Kiro
</p>

<p align="center">
  <img src="./images/login.png" width="49%" alt="Login">
  <img src="./images/dashboard.png" width="49%" alt="Dashboard">
</p>

---

## 🤔 Why AxonRouter-Go?

**Coding agents are amazing — until you try to feed them more than one provider.**

❌ Switching between Claude Code, Codex CLI, Cursor, Cline, and OpenCode means learning five different API formats.

❌ Each provider has its own key, base URL, rate limit, and failure mode.

❌ A single 429 or quota error kills your whole flow.

❌ You have no dashboard to see which connection is healthy right now.

AxonRouter-Go fixes all of that:

✅ **One endpoint** — every tool talks to `http://localhost:3777/v1`.

✅ **18 translation pairs** — hub-and-spoke via OpenAI plus direct translators for known pairs.

✅ **Smart combos** — fall back automatically when a provider is rate-limited, exhausted, or down.

✅ **Circuit breaker** — a failing connection is removed from rotation until it recovers.

✅ **O(1) routing** — pre-computed eligibility snapshot keeps routing under 1 ms regardless of connection count.

✅ **Built-in dashboard** — manage providers, keys, combos, logs, and proxy pools from a browser.

**Never stop coding.**

---

## 🔄 How It Works

```
Your CLI Tool (Claude Code / Codex / Cursor / Cline / OpenCode ...)
│
▼
http://localhost:3777/v1
│
▼
┌──────────────────────────────────────┐
│         AxonRouter-Go                │
│  • Format translation                │
│  • Combo routing + circuit breaker   │
│  • Per-key rate limiting             │
│  • Quota & usage tracking            │
└──────────────────┬───────────────────┘
                   │
   ├─ Subscription ── claude/claude-opus-4.7
   ├─ Cheap backup ── gemini/gemini-2.5-pro
   └─ Free fallback ── oc/qwen-coder-plus
```

1. Your coding agent sends an OpenAI-compatible request.
2. AxonRouter parses the model name (`openai/gpt-4o`, `claude/claude-sonnet-4`, `smart/balanced`, ...).
3. If the model is a **combo**, it walks the priority list until a healthy connection answers.
4. The request is translated to the provider's native format and executed upstream.
5. The response is translated back and returned to your agent.
6. Usage, tokens, and latency are logged to SQLite for the dashboard.

---

## ⚡ Quick Start

### One-line install (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/rickicode/AxonRouter-Go/master/installer.sh | bash
```

The installer detects your OS/arch and installs `axonrouter` into `~/.local/bin` by default.
Use `sudo` or `--to /usr/local/bin` for a system-wide install.

Then run it:

```bash
axonrouter
```

Open http://localhost:3777, log in, add your first connection, and start routing.

### Run once with npx (no install)

If the package is published to npm, you can download and run it once:

```bash
npx axonrouter-go --help
npx axonrouter-go
```

For repeated use or to install a systemd service, use the installer or `npm install -g axonrouter-go` instead.

### Build from source

```bash
# Clone
git clone https://github.com/rickicode/AxonRouter-Go.git
cd AxonRouter-Go

# Install frontend dependencies
cd web && npm install && cd ..

# Build everything
make build

# Run
./build/axonrouter
```

Server starts on port **3777** by default. Dashboard: **http://localhost:3777**.

---

## 🛠️ Supported CLI Tools

| Tool | Notes |
|------|-------|
| **Claude Code** | Set `--api-base-url http://localhost:3777` |
| **Codex CLI** | Set `OPENAI_BASE_URL=http://localhost:3777/v1` |
| **Cursor** | Add custom OpenAI-compatible provider |
| **Cline** | OpenAI-compatible mode |
| **Continue** | OpenAI-compatible provider config |
| **Roo Code** | Same model override as Cline |
| **OpenClaw** | OpenAI-compatible endpoint |
| **Kiro** | OAuth-managed connection in dashboard |
| **Grok Build** | Configure via dashboard CLI tools page |
| **OpenCode** | Free and paid OpenCode prefix support |
| **Cowork** | Claude Desktop 3P / enterprise inference mode |
| **PI Coding Agent** | OpenAI-compatible provider registration |
| **OMP (Oh My Pi)** | OpenAI-compatible provider registration |

> **Any OpenAI-compatible client works.** Point it at `http://localhost:3777/v1` and use provider-prefixed model names.

See [docs/INTEGRATIONS.md](docs/INTEGRATIONS.md) and the dashboard **CLI Tools** page for per-tool copy-paste settings.

---

## 🌐 Supported Providers

| Provider | Prefix | Format | Auth |
|----------|--------|--------|------|
| OpenAI | `openai/` | openai | API key |
| Claude | `claude/` | anthropic | API key / OAuth PKCE |
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
| OpenCode Free | `oc/` | openai | none (free) |
| OpenCode Zen | `oc-zen/` | openai | API key |
| OpenCode Go | `oc-go/` | openai | API key |
| Cloudflare Workers AI | `cf/` | openai | API key |
| ElevenLabs | `elevenlabs/` | openai | API key |
| Deepgram | `deepgram/` | openai | API key |
| Tavily | `tavily/` | openai | API key |
| Brave Search | `brave/` | openai | API key |
| Exa | `exa/` | openai | API key |
| Jina AI | `jina/` | openai | API key |
| Google PSE | `google-pse/` | openai | API key |
| Firecrawl | `firecrawl/` | openai | API key |
| Fal.ai | `fal/` | openai | API key |
| Black Forest Labs | `black-forest-labs/` | openai | API key |
| AssemblyAI | `assemblyai/` | openai | API key |
| Cartesia | `cartesia/` | openai | API key |
| Edge TTS | `edge-tts/` | openai | none (local) |
| Qwen | `qwen/` | openai | API key |
| AliCode | `alicode/` | openai | API key |
| Kimi Coding | `kimi-coding/` | openai | API key |
| iFlow | `iflow/` | openai | API key |
| Volcengine Ark | `volcengine-ark/` | openai | API key |
| Hunyuan | `hunyuan/` | openai | API key |
| Nanobanana | `nanobanana/` | openai | API key |
| Topaz | `topaz/` | openai | API key |
| Puter | `puter/` | openai | API key |
| ComfyUI | `comfyui/` | openai | none (local) |
| Custom OpenAI | `<your-name>/` | openai | API key |
| Custom Claude | `<your-name>/` | claude | API key |
| Cursor | `cursor/` | openai | OAuth (imported from IDE) |
| ZenMux | `zenmux/` | openai | API key |
| ZenMux Free | `zenmux-free/` | openai | API key |
| Grok CLI | `grok-cli/` | grok-cli | OAuth |
| GitHub Copilot | `copilot/` | openai | OAuth |
| CodeBuddy | `codebuddy/` | openai | OAuth |
| Qoder | `qoder/` | openai | OAuth |

Setup details for each provider are in [docs/INTEGRATIONS.md](docs/INTEGRATIONS.md).

---

## 💡 Key Features

| Feature | What It Does | Why It Matters |
|---------|--------------|----------------|
| **Universal Proxy** | One endpoint handles OpenAI, Claude, Gemini, Codex, Antigravity, Kiro, and more. | Stop reconfiguring every tool. |
| **18 Translation Pairs** | Hub-and-spoke + direct translators for known format pairs. | Use Claude clients with OpenAI keys and vice versa. |
| **Combo Routing + Circuit Breaker** | Tries a priority list; gates broken connections with `CLOSED → OPEN → HALF_OPEN`. | 429s, quota errors, and outages don't kill your session. |
| **O(1) Routing** | Pre-computed eligibility snapshot with 50 ms coalesce. | Routing stays under 1 ms at 1,000+ connections. |
| **OAuth Auto-Refresh** | Proactive token rotation for Codex, Antigravity, and Kiro. | No manual re-auth in the middle of a long task. |
| **Per-Key Rate Limiting** | Token bucket per API key or per-IP fallback. | Protect shared setups and public dashboards. |
| **Error Classification** | Auto-detects rate limit, quota exhausted, balance empty, auth failed. | Recovery happens automatically. |
| **Embedded Dashboard** | Svelte 5 SPA served by the Go binary via `go:embed`. | Manage everything from the browser. |
| **Single Binary** | SQLite + frontend + backend in one file. | Drop it on a server and run. |

---

## 💰 Cost Tiers

AxonRouter itself is free (MIT). The table below shows how you can route across real provider price classes inside one combo.

| Tier | Example Providers | Typical Use | Combo Example |
|------|-------------------|-------------|---------------|
| **Subscription** | `openai/`, `claude/`, `cx/` | Daily driver with the best reasoning. | `premium` → use this first. |
| **Cheap** | `deepseek/`, `groq/`, `gemini/` | Fast, capable, cost-sensitive. | `balanced` → subscription first, cheap backup. |
| **Free** | `mimocode-free/`, `oc/`, `cf/` | Prototyping and burn-rate-zero work. | `economy` → free first, paid only if needed. |

Build a combo that fits your budget:

```bash
# Use a balanced combo that falls back across tiers
curl http://localhost:3777/v1/chat/completions \
  -H "Authorization: Bearer YOUR_AXON_KEY" \
  -d '{"model":"smart/balanced","messages":[{"role":"user","content":"hi"}]}'
```

---

## 🎯 Use Cases

### 1. Maximize an existing subscription

You already pay for Claude Pro, OpenAI, and Codex. Route them into one combo so your agent always starts with the best available subscription.

```json
{
  "name": "premium",
  "strategy": "priority",
  "steps": [
    {"model_id": "claude/claude-opus-4.7", "priority": 1},
    {"model_id": "cx/gpt-5.4", "priority": 2},
    {"model_id": "openai/gpt-4o", "priority": 3}
  ]
}
```

### 2. Zero-cost coding stack

For side projects or burn-rate-zero experiments, prefer free providers and only fall back to paid providers when the free tier is exhausted.

```json
{
  "name": "zero-cost",
  "strategy": "priority",
  "steps": [
    {"model_id": "oc/qwen-coder-plus", "priority": 1},
    {"model_id": "mimocode-free/mimo-v2-pro", "priority": 2},
    {"model_id": "deepseek/deepseek-chat", "priority": 3}
  ]
}
```

### 3. 24/7 no-interruption fallback

Combine subscription, cheap, and free tiers into a single combo. If one provider hits a rate limit or quota wall, AxonRouter silently fails over to the next.

```json
{
  "name": "always-on",
  "strategy": "priority",
  "steps": [
    {"model_id": "claude/claude-sonnet-4", "priority": 1},
    {"model_id": "gemini/gemini-2.5-pro", "priority": 2},
    {"model_id": "groq/llama-3.3-70b-versatile", "priority": 3},
    {"model_id": "oc/qwen-coder-plus", "priority": 4}
  ]
}
```

---

## ❓ FAQ

### Is it free?

Yes. AxonRouter-Go is MIT licensed. You bring your own provider keys and pay those providers directly; AxonRouter itself does not charge anything.

### Is it safe to store API keys?

API keys are **bcrypt hashed** in the database. Admin access uses a **JWT session** seeded on first boot; change the default password with `axonrouter --setpass <password>`. The dashboard warns you until the default password is changed.

### How do rate limits work?

You can set a per-key token bucket limit in the dashboard. If no key limit is configured, AxonRouter falls back to a per-IP limit. Upstream rate-limit headers are parsed and respected when available.

### Which free providers work?

MiMoCode Free (`mimocode-free/`), OpenCode Free (`oc/`), and Cloudflare Workers AI (`cf/`) are all supported. Free providers can change rate limits or availability, so combos are strongly recommended.

### Why Go instead of Node?

A single Go binary embeds the SQLite database, the Svelte frontend, and the HTTP server. It starts in under a second, routes in sub-millisecond time, and ships as one file with no runtime dependencies beyond the binary itself.

### Which model should I pick?

Start with a built-in combo (`smart/balanced`, `smart/premium`, etc.) or create your own. If you know exactly what you want, use a provider-prefixed model name like `claude/claude-sonnet-4` or `deepseek/deepseek-chat`.

---

## 📖 Setup Guide

For tool-by-tool copy-paste settings, see [docs/INTEGRATIONS.md](docs/INTEGRATIONS.md).

For full deployment instructions — environment variables, systemd, Docker, upgrading, and performance tuning — see [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md).

Quick links:

[Integrations](docs/INTEGRATIONS.md)
[Deployment Guide](docs/DEPLOYMENT.md)
[API Reference](docs/API.md)
[Architecture](docs/ARCHITECTURE.md)
[Changelog](CHANGELOG.md)

---

## 🔌 API Reference

Proxy endpoints:

- `POST /v1/chat/completions`
- `POST /v1/completions`
- `POST /v1/messages`
- `POST /v1/responses`
- `POST /v1/responses/compact`
- `POST /v1/live`
- `POST /v1/realtime/calls`
- `POST /v1/videos`
- `POST /v1/videos/generations`
- `GET /v1/models`
- `POST /v1/audio/speech`
- `POST /v1/audio/transcriptions`
- `POST /v1/images/generations`
- `POST /v1/images/edits`
- `POST /v1beta/models/{model}:{generateContent,countTokens,streamGenerateContent}`
- `POST /v1/embeddings`
- `POST /v1/unified`

Admin endpoints live under `/api/admin/*` and cover providers, connections, combos, logs, settings, quota, proxy pools, and model pricing.

Full details are in [docs/API.md](docs/API.md).

---

## 🏗️ Architecture

AxonRouter-Go is a single Go binary. A Gin router serves the embedded Svelte dashboard and handles `/v1/*` proxy routes plus `/api/admin/*` admin routes. Internally, a translator hub converts requests between formats, a combo resolver selects the right connection, and an eligibility snapshot grants O(1) routing.

```
┌───────────────────────────────────────────┐
│          AxonRouter-Go Binary             │
│  ┌──────────────┐    ┌──────────────────┐  │
│  │  /v1/* proxy │    │ /api/admin/*     │  │
│  │  translator  │    │ dashboard API    │  │
│  │  executor    │    │ providers, logs  │  │
│  │  combo       │    │ combos, settings │  │
│  └──────────────┘    └──────────────────┘  │
│  ┌─────────────────────────────────────┐  │
│  │  SQLite + background jobs + cache   │  │
│  └─────────────────────────────────────┘  │
└───────────────────────────────────────────┘
```

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full package structure and request flow.

---

## 📦 Deployment & Development

Common Makefile targets:

```bash
make build      # full production binary
make frontend   # build dashboard only
make backend    # build Go binary only
make dev        # frontend hot reload (port 5173)
make run-dev    # dev server on port 3788 with isolated data dir
make test       # run tests
make lint       # run linter
make clean      # remove build artifacts
```

See [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) for systemd, Docker, environment variables, and tuning.

---

## 🛠️ Tech Stack

| Layer | Technology |
|-------|------------|
| **Backend** | Go 1.23 + Gin + SQLite (WAL mode) |
| **Frontend** | Svelte 5 + Vite + Tailwind CSS v4 + shadcn-svelte |
| **Database** | SQLite (embedded, zero config) |
| **Build** | Static frontend embedded via `go:embed` |

---

## 🚀 Latest Release Notes

<!-- LATEST_CHANGELOG_START -->
### What's New in v0.3.22

### Added
- **Kiro live catalog consolidation** — live model catalog fetching, caching, fallback, and variant expansion now live alongside the static catalog in `internal/provider/kiro/catalog.go`, matching the 9Router `kiroModels.js` behavior previously split across `catalog.go` and `models.go`. The helper file `models.go` was removed; live fetch helpers moved into `catalog.go` with the same 5-minute TTL cache and static-catalog fallback. Upstream models are expanded into base / `-thinking` / `-agentic` / `-thinking-agentic` variants with human-readable display names, `rateMultiplier`, and `contextLength`. Curated metadata (Vision/Reasoning/Search capabilities, content strip lists, descriptions) is merged on top of live responses so known models keep their flags. Adds regression tests in `internal/provider/kiro/catalog_test.go` and `models_test.go` for fetch success + cache hit, fallback on failure, and variant expansion.
### Added
- **Provider registry expansion (search, media, Chinese/regional, developer/niche)** — added 22 new built-in provider prefixes to close coverage gaps vs. 9Router/OmniRoute:
- Search: `brave`, `tavily`, `exa`, `jina`, `google-pse`, `firecrawl` (webSearch/webFetch service kinds).
- Media: `fal`, `black-forest-labs`, `assemblyai`, `cartesia`, `edge-tts` (image/video/tts/stt).
- Chinese/regional: `qwen`, `alicode`, `kimi-coding`, `iflow`, `volcengine-ark`, `hunyuan` (LLM).
- Developer/niche: `nanobanana`, `topaz`, `puter`, `comfyui` (image/LLM).
Each provider is seeded in `provider_types` with format `openai`, appropriate base URLs, categories, and service kinds; registered in the OpenAI executor and OpenAI-compatible error translator; aliases added to `provider.Registry`; starter model catalogs appended to `internal/models/models.json`; and STT/TTS executors updated for `assemblyai`, `cartesia`, and `edge-tts`.
- **CommandCode AI provider** — new built-in `commandcode` provider (alias `cmd`) ported from OmniRoute. Adds `internal/executor/commandcode.go` that routes OpenAI-compatible chat completions and streaming to `https://api.commandcode.ai/alpha/generate`, supports the `/provider/v1/models` model list, merges `system`/`developer` messages, clamps `max_tokens` to the upstream ceiling of 200k, and strips empty tool arrays. Registers the provider in `internal/executor/registry.go`, `internal/provider/aliases.go`, `internal/db/migrations.go`, and `internal/api/handlers/v1/models.go`. Seeds 18 CommandCode models in `internal/models/models.json` and capability entries in `internal/models/capabilities.json`. The Svelte frontend catalog and logo were already added in a prior change; this backend completes the integration. Adds `internal/executor/commandcode_test.go`, `internal/provider/aliases_test.go`, and `internal/models/catalog_test.go` coverage.
- **Smart-router virtual models (`smart/auto`, `smart/auto-fast`, `smart/auto-quality`)** — new `internal/smart` package resolves virtual model ids to concrete `provider/model-id` candidates based on request complexity, live telemetry from `request_logs`, provider availability, capability requirements, and API-key allowlists. Virtual model registry is persisted in settings as `smart_router_virtual_models` and consumed by the dashboard Smart Router settings page. Smart routing runs before combo resolution for `/v1/chat/completions`, `/v1/messages`, and `/v1/responses`, with transparent fallback to normal resolution when no eligible candidate is found. Includes `internal/smart/features_test.go` and `internal/smart/router_test.go`.

### Added
- **MCP stdio-SSE bridge for local tool servers** — new `internal/mcp` package registers local MCP servers in an SQLite `mcp_servers` table and exposes them to remote clients via a stdio-SSE bridge. Admin CRUD endpoints (`GET/POST/PATCH/DELETE /api/admin/mcp`, `POST /api/admin/mcp/:id/test`, `GET /api/admin/mcp/:id/tools`) are wired for both session JWT and master API key auth. The SSE endpoint `GET /api/admin/mcp/:id/sse` spawns a per-session subprocess and implements the Anthropic MCP SSE contract (`endpoint` event + `message` events); client messages are delivered via `POST /api/admin/mcp/:id/message?sessionId=xxx`. Subprocesses are reaped by max idle time or disconnect, with restart policies (`always`, `on-failure`, `never`) and a configurable max concurrent clients cap. Command/args are validated to block shell metacharacters; stderr is logged only and never forwarded to clients. A new dashboard page at `/mcp` lists servers, supports add/edit/delete, tests connections, discovers tools, and copies the SSE URL. Added `internal/mcp/mcp_test.go` with CRUD, validation, parsing, and subprocess integration tests. Updated `openspec/specs/api/spec.md` and added `docs/mcp-setup.md`.

- **Provider detail connection-status counters** — the provider detail page now displays fixed status counts next to the Connections heading: total, ready, rate-limited, quota-exhausted, and disabled. Counts are sourced from the provider's existing `status_counts` payload, making it easy to see why a provider's connection pool appears smaller than expected.
- **Connection pagination limits** — provider detail connections now support 200 and 500 items per page (up from the previous 100 maximum). The backend list endpoint accepts per-page values up to 500 and the "Load all" path fetches 500-row pages for faster bulk exports.
<!-- LATEST_CHANGELOG_END -->

See the full [CHANGELOG.md](./CHANGELOG.md) for older releases.

---

## 📜 License

MIT License
