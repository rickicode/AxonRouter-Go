# Changelog

All notable changes to AxonRouter-Go will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
