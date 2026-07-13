# Changelog

All notable changes to AxonRouter-Go will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Copy icon on each model card in the provider detail page to copy the full model name.
- `copyToClipboard` utility with `execCommand` fallback so copy buttons work on plain HTTP deployments.
- `tokens_estimated` column to request logs via database migration, with DB model field and zero-default.
- Fallback token estimator (`internal/usage/fallback.go`) using content-length heuristics for requests and responses.
- `tokens_estimated` badge in dashboard `Logs.svelte` and `tokens_estimated` field in admin API (`RequestLog` interface).
- Per-chunk token extraction from streaming SSE chunks and `MergeTokenCounts` aggregator in `internal/api/handlers/v1/stream.go`.
- `stream_options.include_usage` injection for OpenAI-compatible streaming requests and automatic stripping in all other cases.
- Per-chunk token accumulation in streaming response handler via `StreamTokenCounts` replacing final-chunk-only extraction.
- Fallback token estimation applied in request handlers (chat, messages, responses) when API usage is absent.
- First-login forced password-change flow with backend endpoints (`POST /api/admin/change-password` and `POST /api/admin/defer-password-change`) and a non-dismissable dashboard modal.
- `make test` and `make lint` Makefile targets.
- Multi-stage `Dockerfile` and `.dockerignore`.
- GitHub Actions CI workflow (`ci.yml`) running lint, tests, and frontend build.
- Frontend unit-test harness with Vitest and smoke tests for auth/password API.
- Auto-add missing proxy pool connections UI in OpenCode Free AddConnectionModal.

### Fixed
- Provider detail header: provider name and prefix now sit next to the logo on the left instead of being pushed to the right.
- Responses API `input_tokens`/`output_tokens` no longer zeroed out by the old `TotalTokens` check in `ExtractTokensFromBody`.
- `go test ./...` failure caused by stale `NewProviderHandler` call in `providers_test.go`.
- Context/timer leak in `internal/executor/base.go` reported by `go vet`.
- Untuned HTTP transport (default `MaxIdleConnsPerHost=2`) replaced with pool tuned for high concurrency.
- Bounded routing selection so the hot path samples a constant number of candidates before falling back to a full scan.
- Missing `database.Close()` on graceful shutdown.
- Flaky `TestParseFiltersUsesMilliseconds` assertion that depended on time-of-day.

### Changed
- `ExtractTokensFromBody` extended to parse Gemini `usageMetadata` and OpenAI Responses API `response.usage`/`usage` shapes.
- Usage tracker stores `tokens_estimated` flag in log entries for distinguishing estimated vs actual token counts.
- Documentation now correctly notes that the CLI entry point is planned but not yet shipped.

## [0.3.1] - 2026-07-13

### Added
- Single-source versioning system using `internal/version/VERSION`.
- Build-time version embedding via `//go:embed`.
- Version exposed in startup banner and `/api/admin/health` response.
- Dashboard sidebar now displays the running version and links to this changelog.
- Makefile targets for automated version bump and release (`make release v=X.Y.Z`).
- GitHub Actions release workflow reads version from `internal/version/VERSION`.
- CHANGELOG management rules added to `AGENTS.md`.
