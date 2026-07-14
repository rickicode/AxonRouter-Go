# `/v1/*` Proxy Handler Investigation

## Goal
Trace request flow, identify bugs in `/v1/*` proxy handlers, and return concrete evidence with file paths, line numbers, and code snippets.

## Files to Investigate
- `internal/api/handlers/v1/handler.go`
- `internal/api/handlers/v1/chat.go`
- `internal/api/handlers/v1/messages.go`
- `internal/api/handlers/v1/responses.go`
- `internal/api/handlers/v1/embeddings.go`
- `internal/api/handlers/v1/images.go`
- `internal/api/handlers/v1/tts.go`
- `internal/api/handlers/v1/stt.go`
- `internal/api/handlers/v1/video.go`
- `internal/api/handlers/v1/unified.go`
- `internal/api/handlers/v1/models.go`
- `internal/api/handlers/v1/stream.go`
- `internal/api/handlers/v1/count_tokens.go`
- `internal/api/router.go`

## Type of Bug Checklist
- Nil pointer dereferences
- Missing body drains
- Wrong response status codes
- Missing error propagation
- Bad failover behavior
- Incorrect model routing
- Mishandled streaming
- Auth bypasses
- Concurrency issues