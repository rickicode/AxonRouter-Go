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

The installer detects your OS/arch and installs `axonrouter` into the first writable directory it finds:

1. `~/.local/bin`
2. `/usr/local/bin`

Then run it:

```bash
axonrouter
```

Open http://localhost:3777, log in, add your first connection, and start routing.

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
### What's New in v0.3.4

### Added
- Vertex AI provider (`vertex/` prefix) using Google service-account JSON keys; signs a JWT locally, exchanges it for a Google access token, resolves `{projectId}`/`{location}` base_url placeholders, and proxies OpenAI-compatible `/chat/completions` to Vertex AI's OpenAI endpoint.
- GitHub Copilot provider (`copilot/` prefix) with OAuth-token → Copilot-token exchange, token caching, and the Copilot-specific request headers needed for its OpenAI-compatible `/chat/completions` endpoint.
- System tray mode behind the `tray` build tag. When built with `-tags tray`, `axonrouter --tray` shows a tray icon with menu items to open the dashboard, start/stop the server, and exit. Makefile gains a `build-tray` target; the default build remains headless with no GUI dependencies.
- Quota reset countdown and estimated savings tracker: backend computes next per-provider reset from quota cache, estimates savings from request logs × model pricing, exposes `/api/admin/quota/summary`, and dashboard Quota/Usage pages render global countdown and savings badges.
- OpenAI-compatible providers: added `glm`, `minimax`, `kimi`, `mistral`, `cerebras`, `together`, `fireworks`, `novita`, `lambda`, and `pollinations` prefixes with seeded base URLs, registry routing, catalog keys, and static models for GLM/MiniMax/Kimi/Mistral.
- OpenRouter custom/free model support: dedicated executor wraps OpenAI-compatible requests and preserves configurable `HTTP-Referer`/`X-Title` headers; a cached, no-auth fetch of `https://openrouter.ai/api/v1/models` filters free models by zero prompt/completion pricing and merges them into `/v1/models` so the dashboard always lists current free options; unknown custom model IDs pass through unchanged.
- Amazon Bedrock Mantle provider (`bedrock/` prefix) using the OpenAI-compatible endpoint `https://bedrock-mantle.<region>.api.aws/v1`. The default region is `us-west-2`, overridable via per-connection provider-specific data. Bearer-token auth and bulk connection import are supported.

### Changed
- Replaced Linux-only systemd service installer with cross-platform service management via `github.com/kardianos/service`. `axonrouter --startup {install|install-root|status|start|stop|restart|uninstall}` now works on Linux, macOS, and Windows.
- Service installs preserve the original user's data directory when run under `sudo`; `install-root` installs as root/system instead.

### Fixed
- Default admin password is now the fixed value `12345677` again, and the password-change warning is based on whether the current password still matches the default. Changing the password via `axonrouter --setpass` or Settings clears the warning.
- `/v1/responses`, `/v1/embeddings`, `/v1/images/generations`, `/v1/audio/speech`, `/v1/audio/transcriptions`, `/v1/video/generations`, and `/v1/unified` now enforce the API key lifetime token budget (`max_tokens`) before routing upstream.
- Cloudflare Workers AI model discovery for `/v1/models` is now cached for 5 minutes, preventing an upstream HTTP request on every model-list call.
- Auth cache hardening: `AuthCache.Validate` now stores its own successful result inside the singleflight path; DB query errors are logged instead of swallowed; expired entry deletion rechecks under the write lock to close the TOCTOU window.
- Proxy Pools bulk import now auto-prefixes bare proxy URLs with `http://` when the default type is HTTP and removes the live preview to keep the modal clean.
- Bulk import timeouts for proxy pools and provider connections are extended to 120 seconds to prevent "signal is aborted" errors on large imports.
- Proxy Pools header and tab counters now reflect the total pool count across all pages (via `listAll`) instead of only the current page.
- ComboModal now unwraps the API's `smart_goal` NullString object when editing a smart combo, so subsequent PATCH updates no longer fail with a 400 JSON unmarshal error.
- GitHub Copilot executor now caches the local hosts/apps.json fallback token instead of re-reading from disk on every empty-key request, defaults a missing token `expires_at` to one hour, rejects unsupported endpoints (embeddings/images/responses) with a clear error, and handles Windows config-directory fallback when `LOCALAPPDATA` is unset.
- Google Vertex AI executor now propagates the caller context into the JWT token exchange, enforces a 20-second timeout on the exchange request, and defaults a missing `expires_in` value to 3600 seconds.
- System tray build no longer allows restarting the server after it has been stopped or exited, preventing a panic from reusing a shut down router from the tray menu.
- Combo round-robin strategy no longer panics when `sticky_limit` is 0; it silently clamps to 1.
- Default combo names (`balanced`, `economy`, `premium`) are no longer shadowed by smart goal keywords; regular combos are resolved first.
- Smart combo selection is now deterministic when multiple smart combos share the same goal (sorted by combo name).
- Removed the dead `FallbackRate` threshold in smart `auto` combo selection (the field was never populated).
- Combo routing now replaces the request body's `model` field with each step's actual model before sending upstream, preventing providers from receiving the raw combo name/smart goal.
- Default combo seeding now skips steps that have no matching active connection and discards combos that would end up with zero usable steps, so seeded combos never reference models that cannot be routed.
- Default combo model lists updated to only include providers available out of the box: OC (`oc/hy3-free`), Codex (`cx/gpt-5.4`/`gpt-5.4-mini`/`gpt-5.5`), Cloudflare (`cf/moonshotai/kimi-k2.5`/`kimi-k2.6`/`kimi-k2.7-code`), and Antigravity (`ag/claude-sonnet-4-6`/`ag/claude-opus-4-6-thinking`).
- Fixed base URLs for `novita` (`https://api.novita.ai/openai/v1`) and `pollinations` (`https://gen.pollinations.ai/v1`) so `/v1/chat/completions` resolves to the correct upstream path.
- Fixed Vertex AI static model IDs to use the `google/gemini-...` format required by the OpenAI-compatible Vertex endpoint.
- Fixed Amazon Bedrock Mantle static model IDs by stripping the regional `us.` prefix; Bedrock Mantle expects bare model IDs like `anthropic.claude-3-5-sonnet-...`.
- Added static model catalog sections for `cerebras`, `together`, `fireworks`, `novita`, `lambda`, and `pollinations` so they appear in `/v1/models` without requiring a live connection.
- Added missing dashboard catalog entries (name, color, and icon) for all new providers: `glm`, `minimax`, `kimi`, `mistral`, `cerebras`, `together`, `fireworks`, `novita`, `lambda`, `pollinations`, `copilot`, `vertex`, and `bedrock`.
- Added real brand logo files for new providers: copied existing logos from `9router/public/providers` (`cerebras`, `fireworks`, `kimi`, `minimax`, `mistral`, `together`, `vertex`, `copilot`, `glm`) and downloaded `novita`, `pollinations`, `lambda`, and `bedrock` SVGs.
- Updated `ProviderIcon` to prefer `iconFile` images and fall back to Material Symbols when no image file is available.
<!-- LATEST_CHANGELOG_END -->

See the full [CHANGELOG.md](./CHANGELOG.md) for older releases.

---

## 📜 License

MIT License
