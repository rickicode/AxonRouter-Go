# Provider `grok-cli` — Grok CLI / Grok Build OAuth

## Goal
Add a built-in provider for **Grok CLI / Grok Build** authentication to AxonRouter-Go. The provider routes chat requests through `https://cli-chat-proxy.grok.com/v1/responses` using OAuth tokens and returns standard OpenAI Chat Completions-compatible responses.

## What was built
- **Provider prefix**: `grok-cli` (separate from a future official `xai` API-key provider).
- **Authentication**: xAI OIDC device-code login, plus a manual import flow for access/refresh tokens.
- **Backend packages**:
  - `internal/auth/grokcli` — OIDC discovery, device flow, token refresh (scope now includes `conversations:read conversations:write`).
  - `internal/executor/grokcli.go` — identity headers, request shaping, stable session/turn state, retry logic, and 402 soft-success handling.
  - `internal/translator/openai/grok_cli` — chat-completions ↔ Grok CLI Responses translation.
  - `internal/api/handlers/admin/import_oauth.go` — manual OAuth token import endpoint.
  - `internal/quota/grokcli.go` — upstream billing/user quota fetcher, registered in the quota scheduler.
- **Frontend changes**: `grok-cli` provider card and an OAuth-token import tab in the add-connection modal.

## What is NOT in this version
- No official xAI API-key provider (`api.x.ai`).
- No image/video/media or websocket support for Grok CLI.
- No automatic model discovery from upstream; catalog is static.

## Key design decisions
- Keep Grok CLI as its own prefix so routing, usage, and UI stay clean.
- Reuse existing admin OAuth scaffolding (`internal/api/handlers/admin/oauth.go`) by implementing the `auth.OAuthService` interface.
- Use the existing translator/registry pattern but introduce a new `grok-cli` format so request shaping can differ from Codex.
- Implement a manual import endpoint that can later be reused for other OAuth providers.
- Align chat/quota identity headers with the current official Grok CLI client (`0.2.99`) while preserving unique AxonRouter request shaping.

## Status
Implemented, merged to `master`, and verified. All tasks complete.
