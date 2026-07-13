# Changelog

All notable changes to AxonRouter-Go will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
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

### Fixed
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
- `/v1/embeddings` and `/v1/responses` routes are now mounted and reachable.
- Small error-handling paths hardened: read-body errors return consistent 413 payloads, context cancellations return explicit 499/504, and malformed `provider_specific_data` no longer crashes handlers.
- Removed dead `handleNonStreamResponse` code from `internal/api/handlers/v1/chat.go`.
- Token-bucket refill math fixed for per-minute limits under 60 requests/min, avoiding zero-refill rounding errors.
- Dashboard login is now rate-limited per IP to slow brute-force attempts.
- `ReplaceImageUrls` in `internal/compression/lite.go` now correctly replaces inline data-image URLs and preserves real OpenAI vision `image_url` parts; regex compile errors fail open.
- Version scripts `bump-version.js` and `sync-release-from-tag.js` tolerate existing release sections and always synchronize `README.md`.

### Changed
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
