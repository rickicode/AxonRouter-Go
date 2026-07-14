# Task Report: 03-optional-add-checktokenbudget-to-non-chat-endpoints

**Feature:** v1-system-bug-analysis
**Completed:** 2026-07-14T11:55:29.687Z
**Status:** success
**Commit:** 60d8307cecc6e8c60c3920119a67f6535b2615fc

---

## Summary

Added checkTokenBudget guard to all non-chat /v1 endpoints (responses, embeddings, images, tts, stt, video, unified) so API-key lifetime token budgets are enforced consistently with chat.go/messages.go. Added token_budget_test.go with 7 failing-then-passing tests covering exhausted-budget rejection for each endpoint. `go test -count=1 ./internal/api/handlers/v1/...` passes; full `go test ./...` is blocked by the pre-existing missing web/build embed source, unrelated to these changes.

---

## Changes

- **Files changed:** 8
- **Insertions:** +251
- **Deletions:** -0

### Files Modified

- `internal/api/handlers/v1/embeddings.go`
- `internal/api/handlers/v1/images.go`
- `internal/api/handlers/v1/responses.go`
- `internal/api/handlers/v1/stt.go`
- `internal/api/handlers/v1/token_budget_test.go`
- `internal/api/handlers/v1/tts.go`
- `internal/api/handlers/v1/unified.go`
- `internal/api/handlers/v1/video.go`
