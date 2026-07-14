# Task: 05-optional-harden-auth-cache

## Feature: v1-system-bug-analysis

## Dependencies

- **1. deliver-bug-analysis-report** (01-deliver-bug-analysis-report)

## Plan Section

### 5. (Optional) Harden auth cache
**Depends on**: 1  
**Files**: `internal/api/middleware/auth_cache.go`, `internal/api/middleware/auth.go`  
**What**: Make `Validate` store its own result; log DB errors in `validateKey`; fix TOCTOU delete in `Get`.

## Completed Tasks

- 01-deliver-bug-analysis-report: Bug analysis report already delivered to user in previous turn.
- 01-deliver-v1-bug-report: Completed /v1/ bug analysis report; all tests pass, identified 3 critical and 4 medium/low bugs with file:line evidence.
