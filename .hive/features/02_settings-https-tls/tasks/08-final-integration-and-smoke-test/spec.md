# Task: 08-final-integration-and-smoke-test

## Feature: 02_settings-https-tls

## Dependencies

- **4. Refactor Router to support dual HTTP/HTTPS listeners** (04-refactor-router-to-support-dual-httphttps-listeners)
- **7. Build HTTPS settings tab UI** (07-build-https-settings-tab-ui)

## Plan Section

### 8. Final integration and smoke test
**Depends on**: 4, 7
**Files:**
- Modify: `CHANGELOG.md`
- Modify: `AGENTS.md` if needed (optional)

**What to do:**
- Add CHANGELOG entry under `## [Unreleased]` for Added/Changed.
- Run backend build and tests.
- Run frontend build.
- Smoke test with `make run-dev` and verify:
  - Settings page loads with tabs.
  - HTTPS tab shows public IP and saves YAML.
  - `https.yml` written to `/tmp/axon-dev`.
  - Restarting dev server with enabled config attempts HTTPS on 443 (will fail without domain/port, should log gracefully).

**Verify:**
- `go build ./...` → OK
- `go test ./...` → PASS
- `cd web && npm run build` → zero warnings

## Task Type

modification
