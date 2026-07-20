# AxonRouter-Go

<p align="center">
  <a href="https://github.com/rickicode/AxonRouter-Go/releases/latest">
    <img src="https://img.shields.io/github/v/release/rickicode/AxonRouter-Go?style=flat-square&color=ec4899" alt="Latest Release">
  </a>
  <a href="https://github.com/rickicode/AxonRouter-Go/actions/workflows/release.yml">
    <img src="https://img.shields.io/github/actions/workflow/status/rickicode/AxonRouter-Go/release.yml?style=flat-square&label=release%20build" alt="Release Build">
  </a>
  <img src="https://img.shields.io/badge/Go-1.23%2B-blue?style=flat-square" alt="Go 1.23+">
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
| **OpenCode** | Free and paid OpenCode prefix support |

> **Any OpenAI-compatible client works.** Point it at `http://localhost:3777/v1` and use provider-prefixed model names.

See [docs/INTEGRATIONS.md](docs/INTEGRATIONS.md) for per-tool copy-paste settings.

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
| Custom OpenAI | `<your-name>/` | openai | API key |
| Custom Claude | `<your-name>/` | claude | API key |

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
- `POST /v1/messages`
- `POST /v1/responses`
- `GET /v1/models`
- `POST /v1/audio/speech`
- `POST /v1/audio/transcriptions`
- `POST /v1/images/generations`
- `POST /v1/video/generations`
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
### What's New in v0.3.13

### Added
- **200-tool cap for `grok-cli`** — flattens and truncates large tool lists to the first 200 entries before sending upstream, with a warn log when truncation occurs.
- **Transactional provider-account creation with deduplication, auto-priority, and reorder** — `AddConnection` now runs in a SQLite transaction, rejects duplicate `(provider, name)` or OAuth-token accounts with `409`, auto-assigns priority as `max + 1`, and normalizes priority ordering after every add/delete.
- **Pre-save provider key validation** — backend rejects invalid API keys before persisting the connection; dashboard modal surfaces validation errors inline and blocks submit until the key passes.
- **Per-model account lockout with exponential backoff** — rate-limit/quota errors lock only the failing `(connection, model)` pair, escalate backoff (`30s × 2^level` capped at `1h`), honor upstream `resets_at` timestamps, and clear automatically on success.
- **In-memory device tracker ported from 9router/OmniRoute** — passive tracking on every `/v1/*` request after API-key resolution. Stores SHA-256 fingerprints of masked IP + truncated User-Agent, enforces TTL and per-key/total limits, and exposes no raw IPs.
- **Admin device endpoint and dashboard UI** — `GET /api/admin/keys/:id/devices` returns device count and list; API Keys dashboard page shows count and a detail dialog with fingerprint, masked IP, UA, and last-seen time.
- **OAuth mode in Add Connection modal** — explicit "Connect" button opens the existing backend OAuth flow in a popup, polls until completion, and refreshes the connection list on success.
- **Bulk proxy-pool assignment** — `ProviderDetail` supports multi-selecting connections and applying/unbinding a proxy pool in one transaction; only available for proxy-pool providers (`oc`, `mimocode`).
- **Device-tracker configuration** — env vars `DEVICE_TRACKER_TTL_MS`, `DEVICE_TRACKER_MAX_PER_KEY`, `DEVICE_TRACKER_MAX_TOTAL_DEVICES`.
- **Live Kiro model catalog** — `internal/provider/kiro/models.go` calls `ListAvailableModels` with fingerprint headers, caches results for 5 minutes, falls back to the static catalog, and expands each live model into base / `-thinking` / `-agentic` / `-thinking-agentic` variants carrying `rateMultiplier` and `contextLength`.
- **Kiro multi-endpoint quota fetcher** — `internal/quota/kiro.go` tries `codewhisperer` POST, `codewhisperer` GET, and `q` GET fallbacks; discovers `profileArn` across AWS regions; parses `usageBreakdownList`, `overageConfiguration.unlimited`, and `freeTrialInfo`; surfaces a friendly message for social-auth accounts when quota APIs reject the token.
- **Kiro multi-method authentication** — Kiro now supports AWS Builder ID, IAM Identity Center (IDC), Google/GitHub social OAuth, refresh-token import, API key, and enterprise External IdP (SSO) auth methods. SSRF-guarded enterprise IdP refresh with an allowlist of 15 common IdP host suffixes.
- **Kiro auto-import from local Kiro app** — `internal/auth/kiro/autoimport.go` reads `kiro-cli` SQLite storage, AWS SSO cache, and Kiro IDE `profile.json` to discover existing tokens and profile Arn.
- **Kiro region resolution and auth-aware endpoints** — `internal/executor/kiro_region.go` resolves the runtime region from `profileArn` first, supports only `us-east-1`/`eu-central-1`, orders endpoints per auth method, and sets conditional `tokentype`/`TokenType` headers.
- **Kiro tool schema sanitizer and agentic mode** — tool schemas are sanitized for Kiro's strict JSON Schema subset, long tool names are hash-truncated with reverse name mapping, adaptive thinking is gated to an allowlist of supported models, and synthetic `-agentic` variants receive an agentic system prompt.
- **Kiro inline thinking splitter** — `internal/executor/kiro.go` splits `<thinking>...</thinking>` blocks out of `assistantResponseEvent` content into `reasoning_content` deltas when a separate `reasoningContentEvent` is not emitted.
- **Dashboard simplification + system metrics** — removed date-range selector, defaults to today's traffic only, adds CPU/RAM/disk system-metric cards, and links to the Usage page for details. Backend uses cross-platform `gopsutil`.
- **Usage summary endpoint** — `GET /api/admin/usage/summary` returns today, yesterday, month-to-date, projected month cost, and next quota reset.
- **Usage page enhancements** — replaced the misleading "Saved this month" card with "Cost this month" and "Projected cost", added today vs yesterday deltas.
- **Grok CLI advanced tool normalization** — drops upstream pseudo-tools (`tool_search`, `image_generation`, `apply_patch`), rewrites `custom` → `function`, injects missing parameters, simplifies fragile schemas, auto-injects native `x_search`, normalizes `tool_choice`, and converts legacy `custom_tool_call` / `tool_use` input items.
- **Grok CLI response namespace restoration** — restores original tool names and a `namespace` field on output items so downstream Chat Completions responses stay readable, and filters internal `x_search` subtool traces.
- **Grok CLI reasoning replay cache** — caches replayable output items (reasoning `encrypted_content`, assistant messages, tool calls) per model/session and injects them before the last user message on subsequent turns.

### Fixed
- **Kiro OAuth account naming** — AWS Builder ID / IDC device-code flows and social/import flows now extract the account email from the JWT; when no email is present the connection is named `Kiro-1`, `Kiro-2`, etc.
- **Kiro device-code auto-fill** — the dashboard now receives `verification_uri_complete` so the browser can pre-fill the user code when opening the AWS authorization page.
- **Kiro quota fallback profile ARN** — `internal/quota/kiro.go` now falls back to the shared default `profileArn` for AWS Builder ID and social auth (matching 9router), allowing quota to populate instead of returning "Profile ARN not available".
- **Kiro quota dashboard display** — Kiro credits are shown as `used / total credits` instead of a percentage on the Quota page.
- **Grok CLI non-stream response translation** — `/v1/chat/completions` responses from `grok-cli` are now translated back to standard OpenAI format instead of leaking Grok's internal `response.completed` event shape.
- **Grok CLI tool-call argument streaming** — buffers per-call `function_call_arguments.delta` chunks and falls back to accumulated arguments when `output_item.done` arrives with empty arguments.
- Provider-account single add is now transaction-safe; no more inconsistent in-memory state if DB insert fails.
- Priority gaps after connection deletion are closed by automatic reordering.
- **Grok CLI 402 spending-limit handling** — `personal-team-blocked:spending-limit` responses are now treated as a quota cooldown instead of permanently disabling the connection, so the connection can recover automatically after the user tops up.
- **Grok CLI failover error mapping** — when all `grok-cli` connections are exhausted due to quota/cooldown, the client now receives HTTP 429 `insufficient_quota` with the upstream message instead of HTTP 503 `server_error`.
<!-- LATEST_CHANGELOG_END -->

See the full [CHANGELOG.md](./CHANGELOG.md) for older releases.

---

## 📜 License

MIT License
