# Task: 03-update-changelog-and-smoke-test

## Feature: runtime-settings-total-redesign

## Dependencies

- **2. Reorder Settings tabs and integrate RuntimeSettings** (02-reorder-settings-tabs-and-integrate-runtimesettings)

## Plan Section

### 3. Update CHANGELOG and smoke test
**Depends on**: 2  
**Files:**
- Modify: `CHANGELOG.md`

**What to do:**
- Add entry under `## [Unreleased] / Changed` for the runtime settings redesign.
- Run `make build-dev` and `make run-dev`, open Settings page, verify Runtime tab loads first and settings can be inline-edited.

**Verify:**
- `go test ./...` → PASS
- `cd web && npm run build` → zero warnings
- Smoke test dev server on port 3788 succeeds.

## Task Type

modification
