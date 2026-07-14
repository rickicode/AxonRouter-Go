# Plan: Analisis Bug Sistem `/v1/`

## Discovery
**User request**: analisis sistem `/v1/` apakah ada bug, dengan pemeriksaan mendalam.

**Research findings** (see `context/overview.md` for full evidence):
- Reviewed all `/v1/*` handlers: `chat.go`, `messages.go`, `responses.go`, `embeddings.go`, `images.go`, `tts.go`, `stt.go`, `video.go`, `unified.go`, `models.go`, plus `handler.go`, `stream.go`, and middleware.
- Ran `go test ./internal/api/handlers/v1/... -race -count=1` → passed.
- Ran `go test ./internal/api/middleware/... -race -count=1` → passed.
- Ran `go vet` + `go build ./...` → clean.

## Non-Goals
- This plan is **analysis-only**; no code changes unless explicitly requested.
- We are not adding new endpoints or features.
- We are not refactoring architecture; only surfacing concrete bugs.

## Design Summary / Report
The following bugs are confirmed by direct code inspection:

### Critical
1. **`/v1/responses` missing `writeUpstreamClientError`** — client errors (4xx) route through failover and mark connections as `auth_failed` / `model_not_found`, can trigger auto-disable. Other handlers call this guard first.
2. **`checkTokenBudget` bypass on most non-chat endpoints** — `/v1/responses`, `/v1/embeddings`, `/v1/images/generations`, `/v1/audio/speech`, `/v1/audio/transcriptions`, `/v1/video/generations`, and `/v1/unified` (image/audio/video modes) do not enforce the API key lifetime token budget.
3. **`/v1/responses` bypasses executor retry / auth-refresh path** — calls `exec.ExecuteStream`/`Execute` directly instead of `executeDirect`/`executeWithRetry`.

### Medium / Low
4. **`/v1/models` triggers uncached Cloudflare HTTP fetch on every request** — no TTL/throttle.
5. **`AuthCache.Validate` does not cache its own singleflight result** — callers must `Put`; concurrent misses are inefficient.
6. **`validateKey` silently fails on DB errors** — returns `ok=false` with no logging.
7. **Minor TOCTOU race in `AuthCache.Get`** when deleting expired entries.

## Tasks
### 1. Deliver bug analysis report
**Depends on**: none  
**Files**: `context/overview.md`, `plan.md`  
**What**: Summarize findings with file:line evidence, severity, and recommended fixes.  
**Verify**: Report contains at least the 7 issues above and all line references are accurate.

### 2. (Optional) Fix `/v1/responses` client-error handling
**Depends on**: 1  
**Files**: `internal/api/handlers/v1/responses.go`  
**What**: Add `writeUpstreamClientError` call before `handleFailoverError`, matching `chat.go` behavior.

### 3. (Optional) Add `checkTokenBudget` to non-chat endpoints
**Depends on**: 1  
**Files**: `responses.go`, `embeddings.go`, `images.go`, `tts.go`, `stt.go`, `video.go`, `unified.go`  
**What**: Insert `if h.checkTokenBudget(c, body) != nil { return }` after body read, consistent with `chat.go`/`messages.go`.

### 4. (Optional) Cache Cloudflare model discovery
**Depends on**: 1  
**Files**: `internal/api/handlers/v1/models.go`, `internal/models/catalog.go`  **What**: Add TTL cache (`models.go` or `catalog.go`) so `discoverCloudflareModels` does not hit Cloudflare on every `/v1/models` request.

### 5. (Optional) Harden auth cache
**Depends on**: 1  
**Files**: `internal/api/middleware/auth_cache.go`, `internal/api/middleware/auth.go`  
**What**: Make `Validate` store its own result; log DB errors in `validateKey`; fix TOCTOU delete in `Get`.

