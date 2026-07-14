# Task: 03-optional-add-checktokenbudget-to-non-chat-endpoints

## Feature: v1-system-bug-analysis

## Dependencies

- **1. deliver-bug-analysis-report** (01-deliver-bug-analysis-report)

## Plan Section

### 3. (Optional) Add `checkTokenBudget` to non-chat endpoints
**Depends on**: 1  
**Files**: `responses.go`, `embeddings.go`, `images.go`, `tts.go`, `stt.go`, `video.go`, `unified.go`  
**What**: Insert `if h.checkTokenBudget(c, body) != nil { return }` after body read, consistent with `chat.go`/`messages.go`.

## Completed Tasks

- 01-deliver-bug-analysis-report: Bug analysis report already delivered to user in previous turn.
- 01-deliver-v1-bug-report: Completed /v1/ bug analysis report; all tests pass, identified 3 critical and 4 medium/low bugs with file:line evidence.
