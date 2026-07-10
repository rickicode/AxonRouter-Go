# Adding a Provider to AxonRouter-GO

Use this checklist when adding a new provider to the AxonRouter-GO gateway.
Follow the steps in order. Do not invent URLs, error patterns, sanitization, or
OAuth behaviour — verify against AMRouter (`/workspaces/AMRouter/backend/open-sse`),
OmniRoute (`/workspaces/OmniRoute`), or CLIProxyAPI (`/workspaces/CLIProxyAPI`).

## 1. Register the provider prefix

File: `internal/executor/registry.go`

- Add the provider to `RegisterDefaults`.
- Pick a `ProviderFormat` (`openai`, `claude`, `gemini`, `openai-responses`, etc.).

## 2. Seed the provider type row

File: `internal/db/migrations.go`

- Add a row to the `provider_types` migration slice.
- Use `{placeholder}` in `base_url` when the URL needs per-connection dynamic
  segments (e.g. `{accountId}` for Cloudflare).

## 3. Resolve per-connection URL placeholders

File: the executor that handles the provider (usually `internal/executor/openai.go`)

- Read `req.ProviderSpecificData["accountId"]` (or the relevant key).
- Replace the placeholder before building the final URL:
  ```go
  baseURL = strings.Replace(baseURL, "{accountId}", psd["accountId"], 1)
  ```

## 4. Add provider-specific request sanitization

- Cap `max_tokens` if the upstream imposes limits.
- Strip or convert content block types that the provider rejects.
- Examples: Cloudflare only allows `{type:"text"}` and `{type:"image_url"}` blocks;
  reasoning models cap `max_tokens` at 4096.

## 5. Add error patterns

File: `internal/connstate/patterns.go`

- Append provider-specific text to `QuotaPatterns`, `RateLimitPatterns`,
  `BalanceEmptyPatterns`, or `AuthPatterns` as appropriate.

## 6. Accept ProviderSpecificData in admin APIs

Files:
- `internal/api/handlers/admin/providers.go`
- `internal/api/handlers/admin/connections.go`

- Extend `AddConnection`, `BulkAddConnections`, and related request structs with
  `ProviderSpecificData map[string]string`.

## 7. Pass ProviderSpecificData through test/validation paths

Files:
- `internal/api/handlers/admin/connections.go` (`TestConnection`)
- `internal/api/handlers/admin/providers.go` (`ValidateKey`, `TestAll`)

- SELECT `provider_specific_data` from the DB and pass it to the executor
  request in all three handlers.

## 8. Add the catalog entry

File: `web/src/lib/provider-catalog.ts`

- Add the provider metadata.
- Set `inputFormat: "pipe"` if the bulk-add modal should parse pipe-delimited
  credentials (e.g. `api_key|accountId`).

## 9. Extend the frontend API payload

File: `web/src/lib/api.ts`

- Add `provider_specific_data` to `CreateConnectionPayload`.

## 10. Add pipe-format parsing to the add-connection modal

File: `web/src/lib/components/AddConnectionModal.svelte`

- When `meta.inputFormat === 'pipe'`, split the pasted credential string and pack
  the parts into `provider_specific_data`.

## 11. Add models

Files:
- `internal/models/models.json`
- `internal/models/catalog.go` (add a remote sync endpoint if needed)

- Add the real upstream model IDs only. No aliases, no public-to-upstream
  mapping.

## 12. Add the provider icon

Directory: `web/static/providers/`

- Add an SVG or PNG icon named after the provider prefix.

## 13. If AMRouter already implements the provider, copy it exactly

Mandatory sources to compare against AMRouter:
- `open-sse/config/providers.js`
- `open-sse/config/providerModels.js`
- `open-sse/config/errorConfig.js`
- `open-sse/handlers/chatCore.js`

Match AMRouter behaviour for: URL pattern, request sanitization, `max_tokens`
caps, error patterns, model strip flags (`["thinking"]`, etc.), and provider
alias naming.

## Final checks

- `go vet ./...` and `go build ./...` pass without warnings.
- If the UI changed, `npm run build` in `web/` completes with zero warnings.
- Add a focused integration or unit test that exercises the new provider path
  end-to-end (or with a fake executor for sanitization/logic).
