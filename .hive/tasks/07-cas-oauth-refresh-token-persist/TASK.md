# Task 07 Report — CAS OAuth Refresh Token Persist

## Goal
Centralize compare-and-swap (CAS) persistence of OAuth refresh tokens in the auth manager so callers no longer write tokens directly to the database.

## Implementation Summary
- Added `auth.TokenWriter` interface with `WriteToken(ctx, connID, provider, oldCreds, newCreds) error`.
- Added `auth.Manager.RefreshTokenForConnection(ctx, connID, provider, creds)` for CAS-aware token refresh.
- Implemented `internal/db.OAuthTokenWriter` using a compare-and-swap SQL update so writes are skipped when another writer already rotated the refresh token.
- Wired `db.NewOAuthTokenWriter` into `internal/api/router.go` via `auth.NewManagerWithWriter`.
- Updated call sites to stop writing tokens directly and rely on the auth manager:
  - `internal/api/handlers/v1/handler.go`
  - `internal/api/handlers/admin/connections.go`
  - `internal/api/handlers/admin/models.go`
  - `internal/api/handlers/admin/providers.go`
  - `internal/background/token_refresh_scheduler.go`
  - `internal/quota/fetcher.go`
- Added CAS guard in `internal/auth/codex/token.go` `Store` to avoid overwriting a newer refresh token.
- Updated and expanded unit tests for the CAS flow.

## Verification
```bash
cd web && npm install && npm run build
# build succeeded with zero warnings

go build ./...
# exit 0

go test ./...
# all tests pass
```

## Notes
- Async token writes use a background context inside the write closure because the caller context may cancel before the write queue flushes.
- The existing `RefreshToken` method remains available for code paths that do not have a connection ID.
- `web/package-lock.json` was updated by `npm install` to sync the package version with `internal/version/VERSION` (0.3.19).
