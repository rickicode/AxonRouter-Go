# AxonRouter-Go Architecture

- **User-facing overview & install:** [README.md](../README.md)
- **API endpoint reference:** [API.md](./API.md)
- **Deployment, systemd, Docker, env vars:** [DEPLOYMENT.md](./DEPLOYMENT.md)
- **Tool-by-tool client configuration:** [INTEGRATIONS.md](./INTEGRATIONS.md)
- **Deep technical design / schema:** [TDD.md](./TDD.md)

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
- `cmd/cli` — lightweight CLI for start/stop/status/restart using the PID file (planned, not yet shipped).

### `internal/api/`
HTTP layer built on Gin.

- `router.go` boots all services and mounts `/v1` and `/api/admin` groups.
- `auth_session.go` — `InitAuth` (auto-seeds JWT secret + default admin password), `LoginHandler`, and `SessionAuth` (sliding JWT middleware) for `/api/admin/*`.
- `middleware/` — auth, rate limit, request ID, logging, CORS. The API-key auth middleware (`auth.go`) uses an in-memory `AuthCache` (30s TTL) to skip per-request bcrypt + DB lookups; `AuthCache.Validate` wraps the cache miss in `singleflight` so concurrent misses for the same key collapse into a single bcrypt+DB load (thundering-herd protection at the TTL boundary / cold start).
- `handlers/v1/` — proxy endpoints (`chat/completions`, `messages`, `responses`, models, TTS, STT, images, video).
- `handlers/admin/` — dashboard endpoints for providers, connections, combos, logs, quota, proxy pools, model pricing, and settings.

All handler dependencies are injected via constructors; the v1 `Handler` receives the connection store, eligibility manager, combo handler, usage tracker, auth manager, proxy resolver, quota cache, compression strategy, and exact cache.

### `internal/executor/`
Upstream request execution.

- `base.go` — shared HTTP client, proxy/relay helpers, SSE utilities, and `Executor` interface. The SSE stream scanner goroutine is **ctx-aware**: it `select`s on `scanCh` vs `ctx.Done()` so it exits promptly on client disconnect/backpressure instead of leaking a goroutine per abandoned stream.
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

- `store.go` — `sync.Map`-backed in-memory store keyed by connection ID. `RecordSuccess` fast-paths: if the connection is already `StatusReady` it returns without taking a per-connection write lock, so the hot success path stays lock-free under high throughput.
- `eligibility.go` — pre-computed, atomic snapshot for O(1) provider-prefix lookups. `Update()` rebuilds the snapshot (O(all connections)); `ScheduleUpdate()` **coalesces** concurrent rebuild requests within a 50ms window so bursty failovers (hundreds of concurrent 429s) collapse into one rebuild instead of N. Admin/background paths call `Update()` synchronously when they need a guaranteed immediate rebuild.
- `state.go` — `ConnectionState` with status enum and per-model `ModelLimitState`.
- `detector.go`, `patterns.go`, `headers.go` — error classification, keyword matching, and rate-limit header parsing. Cloudflare Workers AI daily-quota errors (`neurons`, `daily free allocation`, `upgrade to cloudflare`, `4006`) classify as `ErrorQuota` → `StatusQuotaExhausted` with a cooldown that expires at **UTC midnight** (see Quota/Exhaustion).
- `circuit_breaker.go` — `CLOSED → OPEN → HALF_OPEN` state machine.

### `internal/db/`
SQLite layer — WAL mode, busy timeout, foreign keys. The **write path** is split: a centralized `WriteQueue` (`writequeue.go`) funnels non-critical writes (cooldown/ban persistence, OAuth token persistence) through a single draining goroutine (`Enqueue` = non-blocking, drop-on-full, best-effort; `EnqueueOrBlock` = never drops), so request handlers never block on a synchronous DB write lock. Request usage logs are batched separately by the usage tracker (see `internal/usage/`).

Storage internals: `sqlite.go` (singleton connection, migrations runner), `models.go` (Go structs mirroring tables), `key_migration.go` (one-time bcrypt migration for plaintext API keys), `migrations.go` (idempotent schema migrations, provider seeds, pricing seeds, legacy provider ID normalization).

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

- `resolver.go` — four pool types (`http`, `vercel`, `deno`, `cloudflare`), **30-second cache** (`DefaultCacheTTL = 30s`), group strategies (round-robin / sticky). `Resolve()` prioritizes per-connection `proxyGroupId`/`proxyPoolId` (read fresh-ish from the cached pool/group rows), then falls back to the **provider default** stored in the `provider_proxy_defaults` settings key.
- **Hot-reload:** most write paths call `h.resolver.Invalidate()` immediately after mutating pools/groups (e.g. `proxy_pools.go`, `proxy_groups.go`, `proxy_deploy.go`), so create/update/delete via the proxy UI takes effect on the next request. **Known gap:** the generic Settings API write for `provider_proxy_defaults` (`handlers/admin/settings.go`, `PUT /api/admin/settings/provider_proxy_defaults`) and the cleanup blocks that *remove* a provider default (`proxy_pools.go` ~458, `proxy_groups.go` ~303) do **not** call `Invalidate()`. New assignments or removals via those paths lag until the 30s cache TTL expires — no restart required, max ~30s delay.
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


### `internal/config/`
Process-wide configuration singleton.

- Reads `AXON_PORT` and `AXON_ADMIN_KEY`.
- Default data directory is `~/axonrouter`.

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
9. The executor performs the upstream call via HTTP proxy or relay, using the proxy config resolved by `proxypool.Resolver` (cached, 30s TTL; see Proxy Pool Resolution).
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

## Performance Targets & Verified Metrics

| Metric | Target | Verified (this session) | Implementation |
|--------|--------|-------------------------|----------------|
| Routing latency | <1 ms | — | Eligibility snapshot with atomic.Value + 50ms coalesce rebuild |
| Auth cache miss (concurrent, cold) | singleflight | 500 concurrent same-key, 0 failures, ~161 ms | `AuthCache.Validate` + `singleflight.Group` |
| Throughput | hundreds–thousands req/min | 2000 req / 200 workers → 1.565 s (~76k req/min), 0 failures | goroutine per request, async WriteQueue |
| Proxy overhead | <5 ms | — | Minimal middleware, goroutine per request |
| Concurrent streams | 1000+ | — | Goroutine per upstream connection |
| Idle memory | <100 MB | — | sync.Map state + SQLite WAL |
| 5000 connections | <500 MB | — | Compact state structs |
| Startup time | <1 s | — | Embedded assets, no external services required |

## Security

1. **API keys** — bcrypt hashed, fail-closed when configured.
2. **Admin endpoints** — protected by a session JWT (HS256). `POST /api/admin/login` mints a token (initial password is randomly generated on first startup and stored in `admin_password_plain`, changed via `axonrouter --setpass <password>`); the token is sent as `Authorization: Bearer <token>` and slid on every `/api/admin/*` request (idle 72h = logout). `POST /api/admin/health` stays public. The JWT secret (`jwt_secret`) and the bcrypt hash of the default admin password (`admin_password_hash`) are **auto-seeded on first boot** by `InitAuth` (in `internal/api/auth_session.go`): each is written only when its `settings` row is empty, so seeding is idempotent (no-op on later starts) and `setpass` permanently overrides the default. The secret persists across restarts, so issued tokens stay valid. Seeding runs once during `New()` before the server serves traffic, uses atomic single-statement writes, and degrades gracefully (server still boots) if entropy or bcrypt fails — login then just requires a manual `setpass`. Change the default password in production.
3. **Rate limiting** — per-key or per-IP token bucket.
4. **Constant-time comparison** — API key validation.
5. **Relay pools** — auto-generated relay auth tokens.
