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
curl -fsSL https://raw.githubusercontent.com/rickicode/AxonRouter-Go/main/installer.sh | bash
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
### What's New in v0.3.41

### Added
- **Proxy fitness registry + smart rotation (parity 9router)** — proxy pools can now be marked "unfit" for a `provider::model` scope (e.g. Freebuff limited-IP bans) with a 5-minute cooldown:
  - `internal/proxypool/fitness.go` — in-memory `FitnessRegistry` keyed `poolID → scope → mark`, persisted to the `proxy_pool_fitness` settings row (debounced 2s), lazily hydrated on first access, expired marks pruned on read, provider wildcard (`provider::*`) honored by `IsFit`/`FitIDs`.
  - **Smart rotation mode** — new proxy-group mode (`smart`) that filters the group's active pools through the fitness registry for the request's `provider::model` scope before picking (fail-open: falls back to the unfiltered list when every pool is marked unfit). `ResolveCandidates` now takes the model; mode validation accepts `roundrobin|sticky|random|smart`.
  - **Freebuff integration** — on a `limited_ip` pool-scoped error the executor marks the pool unfit (`freebuff::<model>`), the run-loop skips unfit pools, and a successful request clears the mark. Marks survive restarts and are visible/clearable in the new **Proxy Fitness** dashboard page.
  - Admin API: `GET /proxy-pools/fitness` (marks + geo + pool names), `POST /proxy-pools/:id/fitness/clear`, `POST /proxy-pools/fitness/clear-all` (optional provider filter), all registered as static routes before `/proxy-pools/:id`.
  - `web/src/pages/ProxyFitness.svelte` — table of marks with provider/model/pool/egress/reason/cooldown countdown, provider filter + search, per-row and clear-all actions, and a geo-probe enable toggle; sidebar + route wired.
- **Pool egress geo + flapping detection (parity 9router)** — the periodic health probe now captures each pool's egress IP/country/region/city/org through the pool itself:
  - `internal/proxypool/geo.go` — in-memory `GeoCache` with bounded IP history (8), instability flag (≥2 distinct egress IPs, typical for serverless relays), datacenter classification from org regex, and `GEO_PROBE_URL` override.
  - Endpoint fallback chain (`ifconfig.co` → `ipwho.is` → `ip-api.com` → `ipapi.co` → `ipinfo.io`) with a tolerant `parseGeo` handling each shape; manual "test pool" always captures geo, the 30-min health run is gated by the `pool_geo_probe_enabled` setting (default on).
  - `TestResult` gains a `region` field; `TestPoolWithGeo` records into the cache.

- **GitLab Duo, xAI (Grok), and iFlow OAuth providers** — three new built-in OAuth providers closing the parity gap with 9router:
  - `internal/auth/gitlab` — authorization-code + PKCE flow against a configurable GitLab instance (`GITLAB_OAUTH_BASE_URL` / `GITLAB_OAUTH_CLIENT_ID` / `GITLAB_OAUTH_CLIENT_SECRET`), persists `baseUrl`/`clientId`/`authKind` in provider-specific data so token refresh survives env changes, and stores the user profile (`username`, `name`, `email`, `user_id`).
  - `internal/auth/xai` — OIDC discovery (`https://auth.x.ai/.well-known/openid-configuration`) with a static fallback, PKCE + nonce, fixed loopback port 56121, and id_token claim extraction (`email`/`sub`) for account identity.
  - `internal/auth/iflow` — plain authorization_code flow (no PKCE) with Basic auth using the public client credentials, mandatory post-exchange `getUserInfo` call that returns the provider-scoped `apiKey` (stored in `api_key`), plus the account email/phone.
  - `internal/executor/iflow.go` — iFlow request signing: `session-id`, `x-iflow-timestamp`, `x-iflow-signature` (HMAC-SHA256 of `userAgent:sessionID:timestamp` keyed by the apiKey), `User-Agent: iFlow-Cli`, and bearer auth when an apiKey is present; streaming requests get `stream_options.include_usage` injected. Wired into every OpenAI-compatible request path.
  - All three providers are registered in the auth manager, provider aliases, SQLite seeds, and executor/translator registry so they are routable; the dashboard provider catalog and tests cover their metadata.
- **Frontend provider catalog** — `gitlab`, `xai`, and `iflow` entries with OAuth metadata, icons, and test coverage.
- **Translator Debugger UI** — full replay of the request pipeline (client → source → OpenAI → target → provider → client) matching the log files under `logs/translator/`:
  - `internal/api/handlers/admin/translator.go` — `TranslatorHandler` with `Translate` (step 1 detect provider/model/formats, step 2 source→OpenAI, step 3 OpenAI→target + URL/headers/body preview via `BuildUpstreamRequest`), `Send` (streams through the real executor path), and allowlisted `Load`/`Save` of the 7 debug files.
  - `internal/executor/build_request.go` — `BuildUpstreamRequest` mirrors executor request construction (URL, headers, body) without sending, so the debugger can preview exactly what the gateway will emit.
  - `internal/api/handlers/v1/handler.go` — `DebugSend` + `loadConnectionForDebug` export the v1 executor path (proxy handling, streaming, SSE error frames) to the admin debugger without importing the admin package.
  - `web/src/pages/TranslatorDebug.svelte` — 7-step accordion with live streaming into the provider-response step, copy/format/clear/load per step, and `svelte-sonner` toasts; sidebar + route + `translatorApi` client wired in.
  - Unit tests cover format detection (7 cases), endpoint-based source-format overrides, the full step 1–3 round trip against a seeded provider/connection, save/load round trip, and path-traversal rejection.

### Fixed
- **Freebuff direct-leak when all pools blocked** — the resolver's URL-less `direct-fallback` candidate could be attempted by the freebuff executor when a non-strict group's pools were all dead/cooled, leaking the gateway IP. The run-loop now skips any URL-less candidate whenever real pools exist (`len(cands) > 1`), regardless of `StrictProxy`.
- **Usage summary test date sensitivity** — `TestUsageSummaryHandler` in `internal/api/handlers/admin/usage_test.go` failed on the 1st of a month (the month-start row landed in the "today" bucket and the yesterday row fell outside the current month). Expectations are now date-aware.

### Changed

- **Provider icon coverage** — imported the complete public provider icon inventory from the 9router references, mapped missing built-in provider icons, and repaired the corrupted Deepgram asset.
- **Antigravity tool-call uncloak fix (parity 9router)** — OpenAI→client responses now restore the exact original tool name on both stream and non-stream paths. Previously the non-streaming path never stripped the `_ide` cloak suffix (so clients like coding agents received `read_file_ide` instead of `read_file`), and the name-restore helper read the wrong JSON path and dropped sanitization, leaking sanitized names. Replaced `SanitizedToolNameMap` with `CloakedToolNameMap` (`CloakName(SanitizeFunctionName(name)) -> original`), matching 9router's `toolNameMap` and the existing Antigravity→Claude path.
<!-- LATEST_CHANGELOG_END -->

See the full [CHANGELOG.md](./CHANGELOG.md) for older releases.

---

## 📜 License

MIT License
