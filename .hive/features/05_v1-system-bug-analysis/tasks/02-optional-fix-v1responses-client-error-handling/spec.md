# Task: 02-optional-fix-v1responses-client-error-handling

## Feature: v1-system-bug-analysis

## Dependencies

- **1. deliver-bug-analysis-report** (01-deliver-bug-analysis-report)

## Plan Section

### 2. (Optional) Fix `/v1/responses` client-error handling
**Depends on**: 1  
**Files**: `internal/api/handlers/v1/responses.go`  
**What**: Add `writeUpstreamClientError` call before `handleFailoverError`, matching `chat.go` behavior.

## Completed Tasks

- 01-deliver-bug-analysis-report: Bug analysis report already delivered to user in previous turn.
- 01-deliver-v1-bug-report: Completed /v1/ bug analysis report; all tests pass, identified 3 critical and 4 medium/low bugs with file:line evidence.
