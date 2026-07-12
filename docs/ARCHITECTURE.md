# AxonRouter-Go Architecture

## Overview

AxonRouter-Go is a universal AI API proxy delivered as a single Go binary with an embedded SQLite database and a Svelte dashboard. It accepts requests in OpenAI, Anthropic, Gemini, or OpenAI Responses format, routes them to one of many provider connections, and returns a normalized response.

## Design Principles

1. **Single Binary** — No external runtime dependencies; frontend assets are embedded with `go:embed`.
2. **O(1) Routing** — Eligibility snapshot keeps routing latency low regardless of connection count.
3. **Fail-Safe** — Per-connection circuit breaker, automatic failover, cooldown recovery, and quota exhaustion detection.
4. **Observable** — Per-request logging, usage/cost tracking, live active-request panel, and admin dashboard.

## High-Level Architecture

```
Client Request (OpenAI / Claude / Gemini / Responses format)
           │
           ▼
┌─────────────────────────────────────────────────────────────┐
│ Gin Router                                                  │
│  - Auth middleware            (bcrypt API keys)               │
│  - Rate limit middleware      (token bucket)                  │
│  - Logging / Request-ID / CORS                              │
│  - /v1/* → proxy handlers                                   │
│  - /api/admin/* → admin handlers                            │
└──────────────────────────┬──────────────────────────────────┘
           │
           ▼
┌─────────────────────────────────────────────────────────────┐
│ Proxy Handler                                               │
│  1. Parse model string                                      │
│  2. Resolve combo (if model is a combo)                     │
│  3. Pick eligible connection via in-memory snapshot           │
│  4. Apply request compression (chat only)                   │
│  5. Check exact cache (chat, non-stream, no tools)          │
│  6. Refresh OAuth token if needed                           │
│  7. Translate request → provider format                     │
│  8. Execute upstream request (HTTP proxy or relay)          │
│  9. Parse rate-limit headers, classify errors               │
│ 10. Translate response → client format                       │
│ 11. Log usage (async)                                       │
└─────────────────────────────────────────────────────────────┘
```

## Package Structure

### `cmd/`
Entry point binaries.

- `cmd/server` — standalone server that opens the DB, runs migrations, starts background jobs, and runs the HTTP server.
- `cmd/cli` — lightweight CLI for start/stop/status/restart using the PID file.

### `internal/api/`
HTTP layer built on Gin.

- `router.go` boots all services and mounts `/v1` and `/api/admin` groups.
- `auth_session.go` — `InitAuth` (auto-seeds JWT secret + default admin password), `LoginHandler`, and `SessionAuth` (sliding JWT middleware) for `/api/admin/*`.
- `middleware/` — auth, rate limit, request ID, logging, CORS.
- `handlers/v1/` — proxy endpoints (`chat/completions`, `messages`, `responses`, models, TTS, STT, images, video).
- `handlers/admin/` — dashboard endpoints for providers, connections, combos, logs, quota, proxy pools, model pricing, and settings.

All handler dependencies are injected via constructors; the v1 `Handler` receives the connection store, eligibility manager, combo handler, usage tracker, auth manager, proxy resolver, quota cache, compression strategy, and exact cache.

### `internal/executor/`
Upstream request execution.

- `base.go` — shared HTTP client, proxy/relay helpers, SSE utilities, and `Executor` interface.
- `registry.go` — maps provider prefix (`oc`, `cf`, `cx`, …) to a concrete executor + provider format.
- `openai.go`, `claude.go`, `gemini.go`, `codex.go`, `antigravity.go`, `cloudflare.go`, `kiro.go` — provider-specific executors.
- `tts.go`, `stt.go`, `images.go`, `video.go` — modality-specific executors.

The executor layer applies `ProxyConfig` from the request context: standard HTTP proxy transport or relay URL rewriting with `x-relay-*` headers.

### `internal/translator/`
Hub-and-spoke format translation.

- `registry/` — thread-safe registry of request/response translators indexed by `(from, to)` format.
- OpenAI Chat Completions is the canonical hub; providers register direct bidirectional translators.
- `init.go` triggers registration of all translator packages at startup.

### `internal/connstate/`
Core routing state engine.

- `store.go` — `sync.Map`-backed in-memory store keyed by connection ID.
- `eligibility.go` — pre-computed, atomic snapshot for O(1) provider-prefix lookups.
- `state.go` — `ConnectionState` with status enum and per-model `ModelLimitState`.
- `detector.go`, `patterns.go`, `headers.go` — error classification, keyword matching, and rate-limit header parsing.
- `circuit_breaker.go` — `CLOSED → OPEN → HALF_OPEN` state machine.

### `internal/combo/`
Model combo routing.

- `combo.go` — resolves combo names to ordered steps, records success/failure, refreshes from DB.
- `smart_combo.go` — telemetry-driven selection (`auto`, `economy`, `balanced`, `premium`).
- `rotation.go` — round-robin rotation with sticky windows.
- `fallback.go` — circuit-breaker gating over eligible connections.
- `default.go` — built-in combo seeds.

### `internal/auth/`
Authentication.

- `manager.go` — OAuth manager with singleflight dedup, rotation-group serialization, and a 60-second token-rotation cache.
- `codex/`, `antigravity/`, `kiro/` — provider-specific OAuth services.
- API key auth lives in `internal/api/middleware/auth.go` and uses bcrypt.

### `internal/quota/`
OAuth provider quota monitoring.

- `fetcher.go` — fetches quota for Codex, Antigravity, and Kiro with proactive token refresh.
- `cache.go` — persists quota records in `quota_cache` and syncs connection status.
- `exhaustion.go` — in-memory TTL cache for 429-driven exhaustion.
- Provider fetchers: `codex.go`, `antigravity.go`, `kiro.go`.

### `internal/usage/`
Request logging and cost tracking.

- `tracker.go` — async buffered logger; flushes to `request_logs` every 5 seconds or 100 entries.
- `pricing.go` — DB-backed model pricing with prefix stripping and deterministic longest-substring fallback.
- `queries.go` — paginated, filterable request log queries.
- `aggregator.go` — provider/model/daily usage roll-ups.

### `internal/proxypool/`
Proxy and relay pool resolution.

- `resolver.go` — four pool types (`http`, `vercel`, `deno`, `cloudflare`), 30-second cache, group strategies (round-robin / sticky).
- `health.go` — periodic background health checks.
- `test.go` — HTTP CONNECT and relay tests.
- Admin handlers in `internal/api/handlers/admin/` for CRUD, groups, and one-click deploy.

### `internal/models/`
Model catalog.

- `catalog.go` — loads `models.json`, refreshes from remote URLs every 3 hours, syncs live no-auth provider endpoints every 24 hours.
- `models.json` — embedded static catalog keyed by provider prefix.
- Serves `GET /v1/models` and admin model listing.

### `internal/cache/`, `internal/compression/`
Optimization pipelines.

- `cache/exact.go` — bounded in-memory exact-response cache for non-streaming chat completions.
- `compression/strategy.go` — lite baseline compressor + optional Caveman filler stripping; fail-open.
- Compression is applied only on the `/v1/chat/completions` path.

### `internal/db/`
SQLite layer.

- `sqlite.go` — singleton connection, WAL mode, busy timeout, foreign keys.
- `migrations.go` — idempotent schema migrations, provider seeds, pricing seeds, legacy provider ID normalization.
- `models.go` — Go structs mirroring tables.
- `key_migration.go` — one-time bcrypt migration for plaintext API keys.

### `internal/config/`
Process-wide configuration singleton.

- Reads `AXON_PORT`, `AXON_DATA_DIR`, and `AXON_ADMIN_KEY`.
- Default data directory is `~/.axonrouter` unless overridden.

### `internal/active/`, `internal/errorcode/`, `internal/logging/`, `internal/provider/`, `internal/background/`
- `active` — in-flight request registry for the dashboard live panel.
- `errorcode` — extracts numeric status codes from streaming error strings.
- `logging` — global `slog` logger with compact/text/json handlers.
- `provider` — canonical provider ID resolution and legacy alias mapping.
- `background` — quota scheduler, cleanup, and usage-buffer monitoring goroutines.

## Request Flow

1. Client → `POST /v1/chat/completions` (or `/messages`, `/responses`).
2. Middleware validates bearer API key and sets rate limit.
3. Handler parses the model string.
4. If the model matches a combo, the combo handler resolves ordered steps.
5. `getConnection()` uses the eligibility snapshot to find a ready connection.
6. Request body is compressed if compression is enabled.
7. OAuth token is refreshed proactively if close to expiry.
8. The translator converts the request to the provider's native format.
9. The executor performs the upstream call via HTTP proxy or relay.
10. Rate-limit headers are parsed; errors are classified and may trigger cooldown, circuit breaker, or failover.
11. The response is translated back to the client format.
12. Tokens and cost are extracted and logged asynchronously.

## Frontend Architecture

The dashboard is a **Vite SPA** built with Svelte 5 and Tailwind CSS v4, not SvelteKit.

```
web/
├── src/
│   ├── main.ts              # Entry point, mounts App.svelte
│   ├── App.svelte           # Root layout, sidebar, route dispatch
│   ├── app.css              # Tailwind + global tokens
│   ├── pages/               # 16 page components
│   ├── lib/
│   │   ├── api.ts           # Typed fetch wrapper for /api/admin/*
│   │   ├── router.ts        # History-API SPA router
│   │   ├── stores.ts        # Svelte writable/derived stores
│   │   ├── provider-catalog.ts
│   │   └── components/ui/   # shadcn-svelte component port
├── build/                   # Static output embedded by Go
├── embed.go                 # //go:embed all:build
└── vite.config.js           # Dev proxy to Go on port 3777
```

The Go binary embeds `web/build/` and serves it through `http.FileServer`, with a `NoRoute` fallback to `index.html` for the SPA.

## Background Jobs

| Job | File | Interval | Responsibility |
|-----|------|----------|----------------|
| Quota scheduler | `background/quota_scheduler.go` | 1 min | Cooldown recovery, proactive quota fetch, exhaustion cleanup |
| Cleanup | `background/cleanup.go` | 5 min / 24 h | Circuit-breaker sweep, request-logs retention |
| Usage flush monitor | `background/usage_flush.go` | 30 s | Buffer-depth warning |
| Model catalog refresh | `internal/models/catalog.go` | 3 h / 24 h | Remote catalog refresh, live no-auth sync |
| Proxy health checks | `internal/proxypool/health.go` | 30 min | Test all pools and update status |

## Performance Targets

| Metric | Target | Implementation |
|--------|--------|----------------|
| Routing latency | <1 ms | Eligibility snapshot with atomic.Value |
| Proxy overhead | <5 ms | Minimal middleware, goroutine per request |
| Concurrent streams | 1000+ | Goroutine per upstream connection |
| Idle memory | <100 MB | sync.Map state + SQLite WAL |
| 5000 connections | <500 MB | Compact state structs |
| Startup time | <1 s | Embedded assets, no external services required |

## Security

1. **API keys** — bcrypt hashed, fail-closed when configured.
2. **Admin endpoints** — protected by a session JWT (HS256). `POST /api/admin/login` mints a token (default password `12345677`, changed via `axonrouter setpass`); the token is sent as `Authorization: Bearer <token>` and slid on every `/api/admin/*` request (idle 72h = logout). `POST /api/admin/health` stays public. The JWT secret (`jwt_secret`) and the bcrypt hash of the default admin password (`admin_password_hash`) are **auto-seeded on first boot** by `InitAuth` (in `internal/api/auth_session.go`): each is written only when its `settings` row is empty, so seeding is idempotent (no-op on later starts) and `setpass` permanently overrides the default. The secret persists across restarts, so issued tokens stay valid. Seeding runs once during `New()` before the server serves traffic, uses atomic single-statement writes, and degrades gracefully (server still boots) if entropy or bcrypt fails — login then just requires a manual `setpass`. Change the default password in production.
3. **Rate limiting** — per-key or per-IP token bucket.
4. **Constant-time comparison** — API key validation.
5. **Relay pools** — auto-generated relay auth tokens.
