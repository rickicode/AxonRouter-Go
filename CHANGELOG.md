# Changelog

All notable changes to AxonRouter-Go will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- First-login forced password-change flow with backend endpoints (`POST /api/admin/change-password` and `POST /api/admin/defer-password-change`) and a non-dismissable dashboard modal.
- `make test` and `make lint` Makefile targets.
- Multi-stage `Dockerfile` and `.dockerignore`.
- GitHub Actions CI workflow (`ci.yml`) running lint, tests, and frontend build.
- Frontend unit-test harness with Vitest and smoke tests for auth/password API.

### Fixed
- `go test ./...` failure caused by stale `NewProviderHandler` call in `providers_test.go`.
- Context/timer leak in `internal/executor/base.go` reported by `go vet`.
- Untuned HTTP transport (default `MaxIdleConnsPerHost=2`) replaced with pool tuned for high concurrency.
- Bounded routing selection so the hot path samples a constant number of candidates before falling back to a full scan.
- Missing `database.Close()` on graceful shutdown.
- Flaky `TestParseFiltersUsesMilliseconds` assertion that depended on time-of-day.

### Changed
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
