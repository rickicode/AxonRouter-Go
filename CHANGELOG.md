# Changelog

All notable changes to AxonRouter-Go will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Deduplicate OAuth connections by account** — new `internal/db.UpsertOAuthConnection` helper looks up an existing OAuth row by `provider_type_id` + `oauth_email` and updates its tokens, resets `status='ready'` and `is_active=1`, and returns the existing id instead of creating a duplicate. Added the `connections.oauth_email` column and a partial unique index `idx_connections_oauth_account`. Wired the upsert into the generic OAuth callback, Kiro auth flows, OAuth token import, and Codex CLI credential import.

### Changed
- **Account status model refactor** — collapsed the legacy terminal statuses `auth_failed`, `balance_empty`, and `suspended` into `disabled` with a new `disabled_reason` column. Added a non-destructive migration that preserves the cause of existing terminal rows as `manual`, `auth_failed`, `balance_empty`, or `suspended`. Updated connection state, admin connection handlers, token refresh scheduler, quota fetcher, lifecycle cleanup, and dashboard provider summary to set, read, and expose `disabled_reason`. Updated dashboard and provider detail UI so the status distribution, filters, badges, and color helpers only render the canonical statuses (`ready`, `rate_limited`, `quota_exhausted`, `disabled`). Dashboard stats and provider list now count all connections (including disabled), and provider cards expose a `disabled_reasons` breakdown.
- **Global HTTP status-code classification** — moved the unambiguous status-code mappings (`401`/`403` → auth, `408` → timeout, `5xx` → server) into a declarative `StatusCodeCategories` table in `internal/connstate/patterns.go`. Ambiguous codes (`402`, `429`, `404`) still use body-pattern fallbacks so providers cannot force a false classification with a generic status code. Refactored `ClassifyFromResponse` to use the new table and added regression tests.
- **Frontend provider detail test-all** — the `Test all` button on the provider detail page now runs connection tests one-by-one in the browser using the existing `/connections/:id/test` endpoint, refreshing each row inline as it completes instead of calling the backend bulk test endpoint. The per-page selector now includes an `All` option that loads every connection across pages and displays them in a single table.
- **Disabled connections are no longer auto-deleted** — `LifecycleManager.Cleanup` now only removes legacy terminal rows (`auth_failed`, `suspended`, `balance_empty`). Canonical `disabled` rows are preserved and must be deleted manually by the operator.

## [0.3.19] - 2026-07-22

### Added
- **Standalone OAuth token refresh scheduler** — `internal/background/token_refresh_scheduler.go` runs independently of the quota scheduler, scans active OAuth connections, refreshes tokens before expiry, and marks connections `auth_failed` on unrecoverable refresh errors.
- **Forced token refresh retry on quota auth failures** — when a quota fetch fails with an auth error (HTTP 401/403 or equivalent), the quota fetcher performs an unconditional token refresh via `auth.Manager` and retries the fetch once. This applies globally to all OAuth providers.
- **Bedrock tool schema normalization** — strips unsupported JSON Schema keywords (`additionalProperties`, `anyOf`, `oneOf`, `allOf`, `not`, `$schema`, `$id`, `$ref`, `$defs`, `definitions`) from tool schemas sent to Bedrock and ensures every `function.parameters` object has `type: object` with a `properties` map. Required strings that do not match property keys are filtered out, and nested schemas are normalized recursively.
- **API-key allowed_models admin persistence** — `POST /api/admin/api-keys` now accepts `allowed_models`, persists the list as JSON in `api_keys.allowed_models`, and returns it in the creation response. `GET /api/admin/api-keys` parses the stored JSON and includes `allowed_models` in each listed key.
- **API-key allowed_models loaded by auth middleware** — `Auth` now reads `api_keys.allowed_models`, parses it into a set, and stores it both in the Gin context (`allowed_models`) and on the request context via `AllowedModelsFromContext`. Invalid JSON is treated as unlimited.
- **Enforce allowed_models on direct `/v1/*` routes** — all direct routing handlers (`/v1/chat/completions`, `/v1/messages`, `/v1/messages/count_tokens`, `/v1/responses`, `/v1/embeddings`, `/v1/images/generations`, `/v1/video/generations`, `/v1/audio/speech`, `/v1/audio/transcriptions`, `/v1/unified`) now reject requests with `403 Forbidden` when the requested model is not in the API key's allowlist. The check uses the same full-ID and provider-prefix matching as the `GET /v1/models` filter, and a missing or empty allowlist preserves unlimited access.
- **Amazon Q built-in provider** — new `amazon-q/` (and `aq/`) prefix for Amazon Q Developer, reusing the Kiro executor and translator. Registers `amazon-q` in provider types, the executor registry, and the dashboard catalog with a static model list mirroring Kiro (`auto`, Claude 4.x/4.5/4.6/4.7/5, DeepSeek V3.2, MiniMax M2.x, GLM-5, and Qwen3 Coder Next).
- **Kiro Claude 4.6 / 4.7 models** — adds `claude-sonnet-4.6`, `claude-opus-4.6`, `claude-sonnet-4.7`, and `claude-opus-4.7` base models with pricing seed so the full Claude 4.x family is routable.
- **Kiro catalog sync** — keeps `internal/models/models.json` in sync with `internal/provider/kiro/catalog.go` by adding missing `auto` and `claude-sonnet-4` entries, plus a regression test to prevent future drift.
- **QwenCloud built-in provider** — new `qwencloud/` prefix routing to the international DashScope Responses API (`https://dashscope-intl.aliyuncs.com/api/v2/apps/protocols/compatible-mode/v1/responses`) with API-key auth. Uses a dedicated `OpenAIResponsesExecutor` so both `/v1/responses` and translated `/v1/chat/completions` traffic hit the upstream `/v1/responses` endpoint. Includes dashboard catalog entry with the provided Alibaba logo and a seeded model list covering `qwen3.7-plus`, `qwen3.7-max`, `qwen3.6-plus`, `qwen3.6-max`, `qwen3.6-flash`, `qwen3.5-omni-plus`, `qwen-plus`, `glm-5.2`, `deepseek-v4-flash`, and `qwen3-coder-plus`.
- **Filter `GET /v1/models` by API-key allowed models** — `internal/api/handlers/v1/models.go` now reads `allowed_models` from the Gin context (populated by auth middleware in task 02) and restricts the returned model list to entries whose full model ID or provider prefix appears in the set. An empty set preserves the previous unlimited behavior, and the dashboard's `ListActiveModels()` admin route is left unchanged.
- **Dashboard API-key allowlist UI** — the API Keys page now lets operators create keys restricted to specific providers or models. Provider and model multi-selects are built from `providersApi.list()` and `modelsApi.list()`, validation blocks submission with a toast when a restricted mode is chosen with no selection, and the keys table shows a compact summary such as "Limited to 2 model(s)".
- **API-key allowlist regression tests** — expanded unit tests for `filterAllowedModels`, `modelIDAllowed`, and `isModelAllowed` covering unlimited, exact full-ID, provider-prefix, combo, smart virtual model, and negative cases.

### Fixed
- **Kiro social token refresh** — `internal/auth/kiro/social.go` now sends `User-Agent: kiro-cli/1.0.0` during social code exchange and refresh, matching the working 9router flow against Kiro's auth service.
- **Quota refresh passes provider-specific data** — `internal/quota/fetcher.go` forwards `provider_specific_data` strings to `auth.Credentials.ProviderSpecific` so fields like Kiro `profileArn` survive refresh.
- **Tolerant quota refresh** — a failed token refresh no longer disables a connection when the current access token is still valid; the quota fetch continues and defers refresh to the next scheduler tick.
- **Avoid over-clearing exhaustion cache** — `persistSuccess` no longer clears the entire exhaustion cache on every successful request, preserving model-scoped rate-limit entries that may still be active for other models.
- **Deterministic Grok CLI failover test** — `TestGrokCLI_EndToEnd_FailoverAuthFailed` now pins the valid connection as recently-used so the invalid connection is consistently selected first.

## [0.3.18] - 2026-07-20

### Security
- **Restrict master API key break-glass actions** — the programmatic `/admin/api/v1` master key can no longer change the admin password, restart/upgrade the gateway, download/restore backups, read/regenerate itself, or alter TLS config. It remains usable for all other admin-automation endpoints (providers, connections, models, combos, proxy pools, API keys, logs, usage, quota, settings, etc.).

### Changed
- **Routing hot-path optimizations** — reduced `getConnection` ready-snapshot latency by ~4.7× (17.3 µs → 3.7 µs) by capturing `time.Now()` once per candidate, removing redundant `RoutingMode()` lookups from the hot path, converting `ExhaustionCache` to `sync.Map`, and using `singleflight` to collapse concurrent first-time provider-config disk reads.
- **Pre-materialized eligibility readyView** — `EligibilitySnapshot` now stores sorted `*ConnectionState` pointers per provider prefix so the routing loop avoids repeated `store.Get` map lookups.

### Added
- **Routing hot-path benchmarks** — `internal/api/handlers/v1/handler_benchmark_test.go`, `providercfg_benchmark_test.go`, `modellock_benchmark_test.go`, and `exhaustion_benchmark_test.go` baseline the connection-selection path.
- **Per-model round-robin cursor** — round-robin counters are now keyed by `provider + "\x00" + modelID` so independent models on the same provider rotate separately and high-traffic models do not steal rotation from siblings.

## [0.3.17] - 2026-07-20

### Added
- **Console log file rotation and dashboard Console page** — application logs are now written to a rotating on-disk file (`/tmp/axonrouter.log`) via `internal/logging/file.go` (2 MB max, 3 backups). A new `GET /api/admin/console-logs` endpoint tails up to 500 lines for the dashboard, and the new Console page under System shows live, auto-polling log output in the sidebar.
- **Recency-aware connection rotation** — tracks an in-memory `lastUsedAt` timestamp per connection and uses it as a secondary sort key when building the eligibility snapshot and fallback candidate order, spreading simultaneous requests across siblings instead of concentrating them on the same freshly-selected connection.
- **In-product upgrade, logs, and restart flow** — `POST /api/admin/upgrade` now returns per-step upgrade logs, and `POST /api/admin/restart` restarts the service; the About page and update-available modal show live logs and a restart prompt after upgrade completes.

### Fixed
- **Cloudflare Kimi reasoning stream hang** — defaults `chat_template_kwargs.thinking` to `false` for Cloudflare reasoning models unless the client explicitly requests reasoning, and normalizes upstream `reasoning` fields to OpenAI-standard `reasoning_content` in both streaming and non-streaming responses so `cf/moonshotai/kimi-k2.7` no longer appears stuck in thinking.

## [0.3.16] - 2026-07-20

### Added
- **Auto-refresh OAuth tokens during connection test** — expired OAuth tokens are refreshed automatically before `TestConnection` validates the account.
- Docker image build/push moved into the GitHub Actions release workflow.

### Fixed
- **CodeBuddy routing and quota display** — adds a fallback router path for CodeBuddy so requests reach the right translator and surfaces CodeBuddy quota as credits in the dashboard.
- **Kiro OpenAI translator wiring** — wires the OpenAI→Kiro translator, refreshes provider-specific defaults (PSD), and syncs the live model catalog.

## [0.3.15] - 2026-07-20

### Added
- Dashboard update modal with short changelog shown once per browser session when an update is available.
- Sidebar badge on the About menu when an update is available.
- Centralized `web/src/lib/health.ts` store for health/version/update state.
- CLI `--version` / `-v` flag to print the current version without starting the server.

### Changed
- Renamed service-management CLI flag from `--startup` to `--service` (e.g., `axonrouter --service install`).
- `installer.sh` now uses `axonrouter --service install`.
- `POST /api/admin/upgrade` now rejects upgrades unless a newer release is actually available.
- Upgrade now backs up the existing binary to `.bak` before replacing it and restores the backup if replacement fails.
- `version.Checker` is now owned by `Router` and stopped during graceful shutdown.

### Fixed
- **CodeBuddy streaming reasoning leak** — aggregates `reasoning_content` deltas into a single block, strips non-standard SSE noise (`extra_fields`, null `function_call`, empty `refusal`, intermediate `usage`) so clients such as OpenCode don't render choppy "thinking" placeholders.
- `installer.sh` no longer fails with `Init already exists` when upgrading an existing installation; it now reloads and restarts the axonrouter service automatically.
- `Makefile` release target now pushes the `master` branch instead of `main`.

## [0.3.14] - 2026-07-20

### Added
- **CodeBuddy executor wrapper** — adds a dedicated executor for the `codebuddy` provider that prepends a required leading `system` message and always calls the upstream streaming endpoint, aggregating SSE chunks back into a single non-streaming response.

### Fixed
- **Grok 4.5 model catalog limits** — corrected `grok-cli/grok-4.5*` entries in `internal/models/models.json` from 1M context / 65,536 output tokens to 500k context / 32,768 output tokens to match xAI's official Grok 4.5 spec.
- **Grok CLI reasoning cache eviction** — replaced O(n·k) oldest-entry eviction with O(n log n) single-pass sort, added a background goroutine that purges expired replay entries every 5 minutes, and wired start/stop into the router lifecycle.

## [0.3.13] - 2026-07-20

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
- **Grok CLI scanner buffer** — raised the SSE line scanner limit from 64 KB to 50 MB to avoid `bufio.Scanner: token too long` on large streaming events (reasoning replay, tool results, image data).
- **Grok CLI quota info** — fetches Grok task usage (`https://grok.com/rest/tasks/usage`) in addition to the billing/user endpoints, parses `creditUsagePercent`, `creditBalance`, `productUsage`, `onDemandCap`, and weekly/occasional task limits so the Quota page no longer shows "no data" for Grok CLI accounts.
- **Quota page empty refresh** — fixed `Cannot read properties of null (reading 'length')` when a manual quota refresh returns an empty/null quota list.

## [0.3.11] - 2026-07-19

### Added
- **Windows icon + tray launch** — Windows release binary embeds `assets/icon.ico` via `.syso` resources and builds with `-H=windowsgui -tags tray`. Double-clicking `axonrouter-windows-amd64.exe` starts the system tray icon instead of a flashing console.
- **Devin CLI and Qoder providers** — ported from OmniRoute. Devin routes through the local `devin acp` CLI; Qoder supports dual-mode transport (DashScope HTTP for API keys, `qodercli` for PAT `pt-*` tokens). Includes shared CLI subprocess runtime, provider seeding, static model catalog, frontend catalog entries, and alias registry.
- **Devin and Qoder provider icons** — added `devin.svg` (from OmniRoute Windsurf/Cognition branding) and `qoder.png` (from 9router) to the dashboard provider catalog.
- **Built-in `codebuddy` provider (Tencent CodeBuddy)** with custom browser OAuth polling flow, v2 chat endpoint, required Tencent CLI headers, and a 15-model catalog (GLM/Kimi/MiniMax/DeepSeek/Hunyuan).

### Fixed
- **Windows release build** — split Unix process-group logic (`Setpgid`, `Getpgid`, `Kill`) from `internal/executor/cli_runtime.go` into `cli_runtime_unix.go` and `cli_runtime_windows.go`. Windows builds now use `taskkill /F /T /PID` instead of undefined `syscall.Setpgid/Getpgid/Kill`.
- **Public health endpoint no longer runs bcrypt on every request** — `must_change_password` now uses the `admin_password_changed` setting instead of `bcrypt.CompareHashAndPassword`, keeping `/api/admin/health` fast for load-balancer probes and the dashboard sidebar.
- **Version comparison handles pre-release and build metadata** — `internal/version` now parses semver-ish tags such as `v0.4.0-beta.1` or `v0.4.0+build.123` without returning a false "up to date" result.
- **Frontend version helper hardened** — `web/src/lib/about-utils.ts` now ignores pre-release/build suffixes and never returns `NaN` comparison results; stale error state in `About.svelte` is also reset after a successful health fetch.

## [0.3.10] - 2026-07-18

### Added
- **Grok CLI session/turn header persistence** — `/v1/chat/completions` and `/v1/responses` calls routed through `grok-cli` now generate stable `x-grok-session-id`, `x-grok-conv-id`, and `x-grok-agent-id` values per connection, a fresh `x-grok-req-id` per request, and a monotonic `x-grok-turn-idx` that advances only by user messages. State is persisted to the connection's `provider_specific_data` and survives restarts.
- **Grok CLI upstream quota usage** — `grok-cli` connections now display live subscription quota from xAI billing/user endpoints, including monthly included credits, on-demand cap/usage, and prepaid balance. Registered in the quota scheduler alongside Codex, Antigravity, Kiro, and Copilot.
- **Grok CLI identity header alignment** — bumped client version to `0.2.99`, switched User-Agent to `grok-shell/0.2.99 (linux; x86_64)`, and added `x-grok-client-identifier: grok-shell` plus `x-grok-client-mode: headless` to both chat and quota requests. OAuth scope now includes `conversations:read conversations:write`.
- **Grok CLI request normalization** — removed forbidden top-level fields (`presence_penalty`, `frequency_penalty`, `seed`, `user`, `previous_response_id`), converted `custom_tool_call`/`custom_tool_call_output` types, stripped `item_reference` and server-generated IDs that cannot resolve with `store=false`, preserved hosted tool types (`web_search`, `x_search`, etc.), and gated reasoning to models in the `grok-4.5` family (with `max` → `xhigh` mapping).
- **Grok CLI retry and soft-success connection test** — retry transient HTTP 429/502/503 responses with exponential backoff, and treat HTTP 402 during connection tests as a soft success indicating valid auth but exhausted credits.

### Fixed
- **Grok CLI free accounts no longer auto-marked `quota_exhausted`** — quota parsing no longer synthesizes a depleted row for accounts that simply have no on-demand cap (e.g., free/promo accounts). The scheduler only marks a connection exhausted when actual quota data shows zero remaining credits. Matches the 9router reference handler (`open-sse/services/usage/grok-cli.js`).

## [0.3.9] - 2026-07-18

### Added
- **Grok CLI provider (`grok-cli/`)** — seeded `grok-cli` as a built-in OAuth provider with models `grok-build-0.1`, `grok-4.5`, `grok-4.3`, `grok-3-mini`, and `grok-3-mini-fast`, plus representative pricing. Added the dashboard provider card and a `grokcli` OAuth service implementing xAI OIDC device-code discovery, polling, token refresh, and JWT identity parsing.
- **Combo strategies `random` and `least-used`** — `random` picks an unweighted random step per request; `least-used` orders steps by recent successful calls from `request_logs` (cached 30s) so the least-used model is tried first.
- **Combo strategy `fusion`** — parallel panel execution of combo steps followed by a configurable judge model that synthesizes the panel answers. Includes `fusion_config` storage and UI fields for judge model, min panel, straggler grace, hard timeout, and source anonymization.
- **Capability auto-switch for combos** — detects vision, PDF, audio, video, and tool requirements from the request body and reorders combo steps so models that satisfy the required capabilities are tried first. Model capability registry lives in `internal/models/capabilities.json`.
- **Per-combo and global strategy override via Settings** — `combo_strategy` default and `combo_strategies` JSON map allow overriding a combo's strategy at runtime without editing the combo.
- **Seeded default combos** now include `random`, `least-used`, and `fusion` examples and are trimmed to six essential combos instead of eight.
- **Fusion judge model selector** in the dashboard now uses the model picker instead of a free-text input.
- **Strategy reference** moved to a static card at the bottom of the Combos page, explaining every routing strategy and how smart combo selection works.
- **Combo metrics summary cards** on the Combos page show total requests, successes, errors, and average latency across all combos over the last 24 hours.
 - **Database backup and restore** via new `internal/backup` package. Backup always exports all gateway data (API keys, provider accounts/connections, combos, config, request logs, cache) as encrypted-optional JSON Lines. Restore always targets the currently running gateway database.
 - **Clear old logs control** on the Logs dashboard page with 7/30/90 day retention options; preserves `api_key_usage` and other usage summary data.
 - **Colored terminal logs** for the `text` and `compact` log formats, with consistent ANSI coloring of common keys such as `provider`, `conn`, `model`, `status`, `method`, `path`, `client_ip`, and `user_agent`.
 - **Client IP and User-Agent enrichment** for HTTP request logs and upstream executor logs, propagated through request contexts.
- **Stream protection parity with OmniRoute** for combo and direct paths: raw-byte stall detection, adaptive readiness timeout (80s–180s), 750ms/64KB holdback buffer for transparent early retry, and stream-quality peek logging.
- **Combo mid-stream failover**: if an upstream stream fails after the holdback window commits, the combo now falls back to the next eligible connection/model instead of terminating the stream. Only when all candidates fail does the client receive the final SSE `error` + `[DONE]`.
- **Direct-mode mid-stream failover**: `/v1/chat/completions` and `/v1/messages` streaming now also use the 750ms/64KB holdback buffer and retry the next connection if the stream fails after commit.
- `StreamConfig` extended with `StallTimeoutMs`, `HoldbackMs`, `HoldbackBytes`, and `AdaptiveReadiness`.

### Fixed
- Combo strategy override no longer mutates the shared in-memory combo cache, removing a race condition where the original DB strategy could be lost across requests.
- Combo routing is now format-aware for `/v1/messages` and `/v1/responses`: translation and final errors use the correct client API format instead of always assuming OpenAI chat completions.
- Fusion strategy is now restricted to `/v1/chat/completions` until full format-aware judge/response translators are ready for Claude and Responses API.
- Fusion `min_panel` is clamped to the number of steps, so single-step fusion combos no longer fail at runtime with the default config.
- Admin combo creation now validates `fusion_config`; invalid/missing fusion settings fail at creation time instead of at request time.
- Strategy settings (`combo_strategy` / `combo_strategies`) are now cached with a short TTL and validated, eliminating a per-request DB lookup on the combo hot path.
- Capability detection now recognizes Responses API `input_image` and `input_file` content types.
- Streaming context cancellation now emits an in-band SSE `error` event followed by `[DONE]`, preventing clients from hanging when a stream is cancelled.
- Codex (`cx`) connections no longer stay excluded from routing after a DB cooldown recovery: setting a connection back to `ready` now clears any stale in-memory `CooldownUntil`.
- Codex chat requests now use the canonical Codex-specific translator, preventing malformed upstream requests that previously failed with `"Unsupported parameter: max_output_tokens"`.
- Codex streaming/non-streaming responses now use a 50MB SSE scanner buffer (matching CLIProxyAPI) and collect `response.output_item.done` events to patch empty `response.completed` events, fixing empty/truncated responses and `bufio.Scanner: token too long` errors.
- Codex streaming no longer spawns a separate SSE-filter goroutine with an extra channel; filtering of `codex.*` event lines is now done inline in the scanner goroutine. This removes the `send on closed channel` / `close of closed channel` panics seen under failover/retry.
- Codex non-streaming responses now return only the final `response.completed` event (stripped of SSE framing) to the downstream translator, instead of a multi-line SSE dump starting with `response.created`. This fixes client errors where the raw `response.created` object was exposed and failed response-type validation.
- Codex request translation now aligns with 9router/CLIProxyAPI: strips a broader set of unsupported parameters (`frequency_penalty`, `presence_penalty`, `logprobs`, `top_logprobs`, `n`, `seed`, `metadata`, `stream_options`, `safety_identifier`, `prompt_cache_retention`, `previous_response_id`), maps `service_tier` to `priority`, applies a final allowlist, preserves `instructions`/`prompt_cache_key`/`client_metadata`, restricts passthrough tools to known hosted types, and treats `response.done` as an alias for `response.completed`.
- Codex streaming responses now correctly translate upstream `openai-responses` SSE events into OpenAI Chat Completions SSE chunks. The response transform is now registered under the lookup key the streaming handler uses, fixing the bug where raw `event: response.*` lines were forwarded to clients.
- Codex upstream headers are now aligned with CLIProxyAPI: default User-Agent is `codex-tui/...`, Originator is `codex-tui`, `Openai-Beta` and `Codex-Cli-Simplified-Flow` are removed, `Chatgpt-Account-Id` is set from provider metadata or parsed from the access-token JWT, `Session_id` is generated for Mac OS UAs, and `Connection: Keep-Alive` is added.
- Antigravity (`ag`) request envelope is no longer double-wrapped. The executor finalizes the envelope produced by the translator (sets project, request id/session id, request type, strips `request.safetySettings`) instead of wrapping it inside another `request` object.
- Antigravity response transform is now registered in both directions so streaming and non-streaming `/v1/chat/completions` paths can find it.
- Antigravity SSE chunks now include proper `data: ...\n\n` framing, and non-stream requests are routed to `generateContent` instead of `streamGenerateContent`, producing a single translatable JSON response.
- **Backup/restore target confusion fixed:** restore now always writes to the gateway's current database and automatically triggers a graceful shutdown so Docker/systemd can restart the process with fresh caches.
- **Restore reliability improved:** larger insert batches (500 rows), exponential backoff retries on transient `database is locked` / busy errors, and a single per-backup encryption salt so encrypted restores no longer re-derive the PBKDF2 key for every row.

### Changed
- **Simplified Kiro add-connection auth menu** — removed the rarely-used External IDP tile and merged "Import Token" with "Auto-import from kiro-cli" into a single "Import from CLI / Token" flow. Backend support for External IdP and auto-import remains intact; only the dashboard UI was consolidated.
- `streamResponse`, `handleStreamResponse`, and `handleClaudeStreamResponse` now return an `error` so callers can implement retry/failover logic.
- **Backup/restore UI simplified further:** category selection removed; backup always includes every category so restore produces a 100% identical gateway (provider accounts, connections, combos, keys, config, logs, cache).

- **Admin "Test all" now refreshes expired/near-expiry OAuth tokens automatically.** For OAuth providers (`cx`, `ag`, `kiro`, `copilot`), each connection's token is refreshed via `auth.Manager` before testing if it is expired or within the provider's lead time. Test results are recorded with the fresh token, and unrecoverable refresh errors disable the connection as `auth_failed`.
- Docker image now defaults `HOME=/app/data` so the `/app/data` volume is used without manual environment overrides; added GitHub Actions workflow to build and push the container to GHCR on pushes to `master` and version tags.

### Changed
- Improved `Test all` concurrency for providers with thousands of connections: replaced fixed-batch waiting with a semaphore worker pool capped at 10 concurrent streams, plus a 30-second per-connection timeout, so a single slow connection no longer stalls the entire batch.
- **Quota refresh and scheduler now refresh expired OAuth tokens automatically**, including Codex (`cx`) through `auth.Manager`. Previously Codex was skipped, causing quota fetches to fail once the access token expired.

### Fixed
- MiMoCode additional connections now always generate a fresh `accountId`, `accountLabel`, and `fingerprint`, ensuring each connection behaves as a distinct logical account and does not reuse a device identity that could trigger MiMoCode anti-abuse controls.
- MiMoCode bootstrap now respects the same proxy context as chat requests, preventing `Invalid Token` errors caused by JWTs being issued from the server's direct IP but used through a proxy pool IP.
- MiMoCode "high-frequency non-compliant requests" 400 errors are now classified as rate-limit signals, so flagged proxy/account combinations are auto-cooldowned and skipped during routing.
- MiMoCode connections configured with a proxy pool no longer fall back to the server's direct IP, preventing direct-IP rate-limit cascades during TestAll and failover attempts.
- Split `mimocode` (free tier), `mimo` (PAYG), and `mimo-tp` (token plan) into three distinct providers with separate provider types, executors, model catalogs, and dashboard icons. Previously `mimo` was aliased to `mimocode`, causing PAYG requests to be routed through the no-auth free endpoint.
- Dashboard **Usage** page now shows a skeleton loader while fetching usage data instead of immediately displaying a session-expired card.
- Dashboard **API Keys**, **Developers**, and **Provider Add** pages now use skeleton loaders during their initial data fetch instead of plain text or no placeholder.
- `axonrouter --startup install-root` no longer incorrectly rejects root execution after internally remapping the action to `install`.
- GitHub Actions npm publish job now syncs the wrapper package version from the release tag, preventing stale `package.json` versions from being published again.

<!-- Add new entries above this line -->

## [0.3.7] - 2026-07-16

### Fixed
- Admin handlers now use request context instead of `context.Background()`, allowing client cancellation to propagate to background operations (connection testing, provider testing, model sync, proxy pool deletion).
- Startup banner now logs warnings for database query failures instead of silently ignoring them.
- OpenAI-format provider errors are now normalized to canonical OpenAI error codes (`context_length_exceeded`, `rate_limit_exceeded`, etc.) via a default translator. This fixes Bedrock `validation_error` and other provider-specific synonyms so CLI tools like Claude Code and OpenCode correctly trigger auto-compact.
- Cloudflare Workers AI plain-text context-window errors (e.g. "exceeded this model context window limit") are now correctly translated to `context_length_exceeded`.
- Streaming upstream errors (`OpenAIExecutor`, `CopilotExecutor`, `MimocodeExecutor`, `AntigravityExecutor`, `GeminiExecutor`) are now translated using the provider prefix, matching non-streaming behavior.
- Claude `model_context_window_exceeded` stop reason is now mapped to OpenAI-compatible `finish_reason: "length"` so clients can detect context-window truncation.
- Fixed Claude → OpenAI non-streaming response translation: `finish_reason` is now placed at the choice level (matching the OpenAI spec) instead of inside `message`.

### Changed
- Extracted `unifiedSurface` helper for consistent API surface labeling in logging and in-flight request tracking.
- Strengthened `InferCodeFromMessage` phrase detection for context-length errors (`input_tokens`, `reduce the length`, `prompt contains`, `context window`, `context exceeds`, etc.) and shared it across all provider translators.
- Per-provider `Compatibility` config in `internal/providercfg` with seeded defaults for Cloudflare (`cf`) and Bedrock (`bedrock`). Provider-specific OpenAI-compatible quirks (model prefix, max_tokens cap, content-array flattening, reasoning_effort allow-list, provider-prefix strip) are now configurable without rebuilding.
- Claude → OpenAI streaming response translator now forwards `usage` in the final chunk so clients can track token consumption.
- Claude → OpenAI streaming response translator now uses statically-typed event structs instead of per-event `map[string]interface{}`, reducing allocations by ~78% (171 → 38 allocs/op) and latency by ~63% (23767 → 8704 ns/op) while producing byte-identical output.

## [0.3.6] - 2026-07-16

### Added
- Request logs now record the client-facing API type (`api_type`) so the `/logs` dashboard shows whether a request came through the OpenAI-compatible surface (`/v1/chat/completions`), Claude surface (`/v1/messages`), responses, embeddings, images, audio, video, or other endpoints.
- API key expiration with 1/7/30/90 days, custom date, and no expiration options.
- `AXONROUTER_DIR` environment variable overrides the data directory (default remains `~/axonrouter`). Relative paths resolve against `$HOME`.

### Fixed
- Auth middleware now uses `AbortWithStatusJSON` instead of writing a body and then aborting, preventing malformed responses on auth-system errors.
- `TrackActive` middleware no longer consumes and re-reads multipart bodies (used by `/v1/audio/transcriptions`), so STT requests are no longer corrupted by the in-flight request tracker.

### Changed
- `installer.sh` now installs the binary into `~/.local/bin` by default and prints clear `sudo`/`--to` instructions when that directory is not writable.
- `npm/axonrouter-go` postinstall now copies the verified binary to `~/.local/bin/axonrouter` on Linux/macOS when possible, falling back to the package-local binary with instructions.
- `axonrouter --startup install` now creates a systemd **user** service (`~/.config/systemd/user/axonrouter.service`) and no longer requires root. Running it as root is blocked; use `axonrouter --startup install-root` for a system-wide service.
- Docs now mention `npx axonrouter-go` as a one-off, no-install way to run the binary once the package is published.

## [0.3.5] - 2026-07-15

### Fixed
- Removed duplicate MiMoCode Free provider from the dashboard: `mimocode-free` is now normalized to the canonical `mimocode` alias on startup (connections, quota cache, and custom models are migrated; the legacy provider_type row is deleted).
- GitHub Copilot OAuth account creation no longer fails with "Connection not ready nil credentials" / "empty access token". GitHub's device-code endpoint returns HTTP 200 with `error: authorization_pending`; that response is now recognized so polling continues. Copilot token prefetch failures are non-fatal (matching OmniRoute), and real terminal errors are propagated to the UI instead of showing the generic "nil credentials" message.
- GitHub Copilot quota tracking is now implemented. The quota scheduler fetches usage from `https://api.github.com/copilot_internal/user`, parses both paid (`quota_snapshots`) and free/limited (`monthly_quotas` + `limited_user_quotas`) response formats, and auto-refreshes the short-lived Copilot token before each fetch so the dashboard no longer shows "No quota data".
- GitHub accounts without Copilot access now fail add-account with a clear message: "this GitHub account does not have GitHub Copilot access", instead of a raw 403 JSON blob. The same message is used by the quota scheduler to disable the connection.
- GitHub Copilot OAuth now falls back to the GitHub login/name when the `/user` response does not include an email, so the dashboard connection label shows the actual account instead of "OAuth GitHub Copilot".
- Added a guard in the quota fetcher so any provider added to `knownProviders` without a matching fetcher case returns a clear error instead of silently showing "No quota data".
- Provider detail model list now inherits `service_kinds` from the provider when a model has no per-model kind metadata. Fallback is restricted to single-kind providers so multi-modal providers (e.g., Cloudflare) are not blanket-tagged with every capability.
- Provider cards on the /providers page now display their category badge (e.g., "OAuth", "API Key", "No Auth", "Service Account") so every provider has visible category metadata, matching the category badge shown on Provider Detail.
- Quota scheduler now prunes stale `quota_cache` rows when a connection is no longer an active OAuth connection, so deleted/disabled Copilot attempts stop appearing as duplicate error cards.
- Fixed GitHub Copilot quota fetch to call `/copilot_internal/user` with the GitHub OAuth access token (`Authorization: token …`), not the short-lived Copilot token — this was the root cause of `401 Bad credentials` and now matches OmniRoute.
- Fixed free/limited Copilot quota parsing: `limited_user_quotas[name]` is the *remaining* count, not the used count (previously usage percentages were inverted).
- Proactive OAuth token refresh now refreshes Copilot tokens even though GitHub device-code flow doesn't return a refresh token; the manual admin refresh endpoint also supports Copilot and persists the refreshed Copilot token to `provider_specific_data`.

### Added
- MiMoCode Free provider (`mimocode/` prefix) with dedicated `MimocodeExecutor`: per-device-fingerprint JWT bootstrap, anti-abuse system marker, required `x-mimo-*` headers, one-time 401/403 retry, and proxy-pool selection for non-default connections. Includes a seeded `mimocode-direct-default` connection and backend validation/rules mirroring OpenCode Free.
- Updated static GitHub Copilot model catalog in `internal/models/models.json` to include newer generally-available models: `claude-opus-4.6`, `gpt-5.4-nano`, `gpt-5.6-luna`, `gpt-5.6-sol`, `gpt-5.6-terra`, `gemini-2.5-pro`, and `gemini-3-flash-preview`.
- Quota dashboard provider summary now returns per-provider color/icon metadata and the provider filter pills use those colors, so Copilot and other providers render with their brand color instead of default gray.
- Track and display real compression metrics: per-mode counters (`requests`, `original_tokens`, `compressed_tokens`) are recorded from live `/v1/*` requests via the write queue, exposed via `GET /api/admin/compression/metrics`, and rendered in a new "Compression Metrics" card on the Optimization page.

## [0.3.4] - 2026-07-15

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

## [0.3.3] - 2026-07-14

### Added
- Native HTTPS on port 443 via Let's Encrypt (`golang.org/x/crypto/acme/autocert`) configured from the dashboard Settings → HTTPS tab.
- Admin TLS API endpoints (`/api/admin/tls-config`, `/api/admin/tls-config/public-ip`, `/api/admin/tls-config/check-dns`) for HTTPS setup.
- `internal/config/https.go` persists HTTPS config to `https.yml` and router starts dual HTTP/HTTPS listeners.
- Public IP detection helper `internal/network/publicip.go` with `AXON_PUBLIC_IP` override and fallback lookup.
- `category` and `service_kinds` columns on `provider_types`, exposed in admin provider List/Get responses.
- `internal/provider/servicekind.go` constants (`llm`, `embedding`, `image`, etc.) and helpers `HasServiceKind`/`DefaultServiceKinds`.
- Static per-modality registry (`internal/modalities`) using embedded JSON, with a Cloudflare pilot covering canonical embedding and image model IDs.
- `service_kinds` field on model catalog entries, included in `/v1/models`, admin models, admin provider model list, and admin model-pricing responses.
- Cloudflare Workers AI routing for `/v1/embeddings` and `/v1/images/generations` through executor adapters and per-modality model gating.
- Dashboard provider category/service-kind chips and modality badges in the model picker, model pricing, and provider detail pages (LLM-only badges hidden).
- `must_change_password` flag returned by `/api/admin/health`; the dashboard warning is driven entirely by this endpoint.
- Dedicated change-password card on the Settings page with current/new/confirm password fields.
- Copy icon on each model card in the provider detail page to copy the full model name.
- `copyToClipboard` utility with `execCommand` fallback so copy buttons work on plain HTTP deployments.
- `tokens_estimated` column to request logs via database migration, with DB model field and zero-default.
- Fallback token estimator (`internal/usage/fallback.go`) using content-length heuristics for requests and responses.
- `tokens_estimated` badge in dashboard `Logs.svelte` and `tokens_estimated` field in admin API (`RequestLog` interface).
- Per-chunk token extraction from streaming SSE chunks and `MergeTokenCounts` aggregator in `internal/api/handlers/v1/stream.go`.
- `stream_options.include_usage` injection for OpenAI-compatible streaming requests and automatic stripping in all other cases.
- Per-chunk token accumulation in streaming response handler via `StreamTokenCounts` replacing final-chunk-only extraction.
- Fallback token estimation applied in request handlers (chat, messages, responses) when API usage is absent.
- Password-change warning driven by `/api/admin/health` only; admin endpoints are no longer blocked when the default password has not been changed.
- `make test` and `make lint` Makefile targets.
- Multi-stage `Dockerfile` and `.dockerignore`.
- GitHub Actions CI workflow (`ci.yml`) running lint, tests, and frontend build.
- Frontend unit-test harness with Vitest and smoke tests for auth/password API.
- Auto-add missing proxy pool connections UI in OpenCode Free AddConnectionModal.
- Searchable, scrollable proxy pool selector in OpenCode Free AddConnectionModal.
- Settings page now groups runtime settings by category and supports live search.
- Proxy pool bulk select with row checkboxes, bulk test/delete toolbar, and "Delete all error/timeout" confirmation.
- Unified "Add pool" dialog with Single/Bulk tabs; bulk import supports healthy-only filtering and optional <1s response-time filtering.
- `POST /proxy-pools/bulk-delete` endpoint supporting deletion by `ids` or `test_status`.
- Cached GitHub latest-release checker in `internal/version/upgrade.go` with a 5-minute in-memory cache and zero-dependency semver comparison.
- `GET /api/admin/health` now returns `latest_version` and `update_available` via the version checker.
- `POST /api/admin/upgrade` endpoint downloads the platform-specific release binary, verifies its SHA256 against `checksums.txt`, and writes it to `<DataDir>/bin/<asset>`.
- About page (`/about`) with project summary, repository link, version card, changelog section, and System sidebar entry.
- `web/src/lib/about-utils.ts` utilities for version normalization, semver comparison, and changelog parsing, with Vitest coverage.
- Release workflow now generates and attaches `build/checksums.txt` with SHA256 sums for all binary artifacts.
- About page polls `/api/admin/health` every 30s for update availability and posts to `/api/admin/upgrade` with toast feedback and a loading spinner.
- `random` mode for proxy groups; resolver selects a uniform random active pool per request.
- Bulk pool selector in the Proxy Pools → Groups create/edit modal: large scrollable table, select-all/clear, test selected/test all, select lowest latency, and select healthy.
- `proxyPoolsApi.listAll()` frontend helper that fetches all proxy pools across paginated pages.
- `proxy_pool_id` column on `request_logs` with migration; every v1 request handler records the resolved proxy pool ID.
- Request-log queries join `proxy_pools` and return `proxy_pool_name` so the dashboard can show direct vs proxy routing.
- Logs page latency column now shows `direct` or the proxy pool name for each request.

### Fixed
- `/v1/embeddings` and `/v1/images/generations` now validate the provider's service kind and, for Cloudflare, require a registered per-modality model before routing.
- Migration ensures the built-in `openai` provider type keeps `embedding` and `image` service kinds so existing OpenAI embeddings and DALL-E routing continue to work.
- Provider detail header: provider name and prefix now sit next to the logo on the left instead of being pushed to the right.
- Responses API `input_tokens`/`output_tokens` no longer zeroed out by the old `TotalTokens` check in `ExtractTokensFromBody`.
- `go test ./...` failure caused by stale `NewProviderHandler` call in `providers_test.go`.
- Context/timer leak in `internal/executor/base.go` reported by `go vet`.
- Untuned HTTP transport (default `MaxIdleConnsPerHost=2`) replaced with pool tuned for high concurrency.
- Bounded routing selection so the hot path samples a constant number of candidates before falling back to a full scan.
- Missing `database.Close()` on graceful shutdown.
- Flaky `TestParseFiltersUsesMilliseconds` assertion that depended on time-of-day.
- `/v1/*` auth is now fail-closed: missing/invalid API keys always return 401 instead of slipping through.
- Request bodies larger than 10 MB are rejected with 413 before reaching downstream handlers; the original body is preserved for the normal path.
- Exact response cache now only stores upstream responses with 2xx status codes; errors are no longer cached.
- Non-chat handlers (`/v1/images/generations`, `/v1/video/generations`, `/v1/audio/*`, `/v1/embeddings`) now pass through the real upstream HTTP status and body instead of masking them as 502.
- `/v1/responses` now mirrors `/v1/chat/completions` and passes through non-retryable upstream client errors (e.g., 400 context length) instead of failing over to a generic 503.
- `/v1/embeddings` and `/v1/responses` routes are now mounted and reachable.
- Small error-handling paths hardened: read-body errors return consistent 413 payloads, context cancellations return explicit 499/504, and malformed `provider_specific_data` no longer crashes handlers.
- Removed dead `handleNonStreamResponse` code from `internal/api/handlers/v1/chat.go`.
- Token-bucket refill math fixed for per-minute limits under 60 requests/min, avoiding zero-refill rounding errors.
- Dashboard login is now rate-limited per IP to slow brute-force attempts.
- `ReplaceImageUrls` in `internal/compression/lite.go` now correctly replaces inline data-image URLs and preserves real OpenAI vision `image_url` parts; regex compile errors fail open.
- Version scripts `bump-version.js` and `sync-release-from-tag.js` tolerate existing release sections and always synchronize `README.md`.

### Changed
- Settings page redesigned into Runtime, Security, and HTTPS tabs with Runtime as the default tab.
- Default data directory moved from `~/.axonrouter` to `~/axonrouter`; the `AXON_DATA_DIR` environment variable is no longer read or documented.
- Systemd service installed by the binary (`axonrouter --startup install`) and by `installer.sh --service` no longer sets `AXON_DATA_DIR`; it relies on the binary default relative to the service user's home directory.
- Binary CLI now supports `--help`, `--startup {install|status|start|stop|restart}` for systemd management, and `--setpass <password>`.
- Installer automatically appends the install directory to `~/.bashrc`/`~/.zshrc` when it is not already on `PATH`.
- Runtime settings fully redesigned as a clean list/table with category filter pills, search, and inline edit; non-runtime keys (CLI Tools, API Key, etc.) are no longer shown in the Runtime tab.
- Optimization dashboard page redesigned: tabs now use pill-style controls matching ProxyPools, and the Cache tab gained a header row with refresh/flush actions, proper stat cards for hits/misses/hit rate/entries, plus a clarification note explaining cache eligibility for non-streaming/tool/cache_control responses.
- `ExtractTokensFromBody` extended to parse Gemini `usageMetadata` and OpenAI Responses API `response.usage`/`usage` shapes.
- Usage tracker stores `tokens_estimated` flag in log entries for distinguishing estimated vs actual token counts.
- Documentation now correctly notes that the CLI entry point is planned but not yet shipped.
- Default admin password is now randomly generated on first startup and stored in `admin_password_plain`; the initial hardcoded password has been removed. A warning is shown on the dashboard until the password is changed.

## [0.3.1] - 2026-07-13

### Added
- Single-source versioning system using `internal/version/VERSION`.
- Build-time version embedding via `//go:embed`.
- Version exposed in startup banner and `/api/admin/health` response.
- Dashboard sidebar now displays the running version and links to this changelog.
- Makefile targets for automated version bump and release (`make release v=X.Y.Z`).
- GitHub Actions release workflow reads version from `internal/version/VERSION`.
- CHANGELOG management rules added to `AGENTS.md`.
