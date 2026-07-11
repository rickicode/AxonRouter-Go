# Provider Error Translation Plan — OpenAI-Compatible Error Responses for AxonRouter-GO

## Goal
Setiap provider yang terdaftar di `internal/executor/registry.go` harus mengembalikan error response dalam format **OpenAI-compatible** (`{"error":{"message","type","code"}}`) dengan HTTP status yang benar. Sistem harus lebih efisien dan lebih stabil daripada implementasi di OmniRoute.

## Current State (verified)
Saat ini **hanya Cloudflare** yang punya translator:
- `internal/executor/cloudflare_error.go` → translate Cloudflare Workers AI envelope ke OpenAI error.
- `internal/executor/cloudflare.go` → wrap `Execute`/`ExecuteStream` supaya mengembalikan `*executor.UpstreamError`.
- `internal/api/handlers/v1/handler.go` → `writeUpstreamClientError()` langsung menulis 4xx error ke client (kecuali 429 tetap failover).

Provider lain masih melempar raw string, contoh:
- `claude.go`: `fmt.Errorf("claude error %d: %s", resp.StatusCode, string(resp.Body))`
- `gemini.go`: `fmt.Errorf("gemini error %d: %s", resp.StatusCode, string(resp.Body))`
- `antigravity.go`: `fmt.Errorf("antigravity error %d: %s", resp.StatusCode, string(resp.Body))`
- `codex.go`: `fmt.Errorf("codex stream error: %w", chunk.Err)`
- `kiro.go`: `fmt.Errorf("kiro error %d: %s", resp.StatusCode, string(resp.Body))`

Akibatnya client/AI agent hanya menerima `HTTP 503 {"error":{"message":"all connections exhausted...","type":"server_error"}}`.

## Comparison with OmniRoute

### What OmniRoute does well
- `open-sse/utils/error.ts` → `parseUpstreamError()` parses JSON upstream body dan ekstrak: `statusCode`, `message`, `retryAfterMs`, `errorCode`, `errorType`, `responseBody`.
- `buildErrorBody()` → membuat response OpenAI-compatible: `{"error":{"message","type","code"}, "upstream_details":{...}}`.
- `open-sse/config/errorConfig.ts` → memetakan HTTP status ke OpenAI `type`/`code` default (400→invalid_request_error/bad_request, 429→rate_limit_error/rate_limit_exceeded, dll).
- `open-sse/handlers/chatCore.ts` → call `parseUpstreamError()`, lalu `classifyProviderError()` untuk memutuskan fallback/cooldown.

### OmniRoute weaknesses we can beat
1. **String-heavy parsing**. `parseUpstreamError` harus `await response.text()` lalu `JSON.parse(text)`, `String(...)`, `sanitizeErrorMessage()` dengan tokenisasi regex.
2. **No special handling for Cloudflare nested envelope**. Cloudflare `{"errors":[{"message":"AiError: AiError: {...}"}]}` tidak dibongkar khusus; message jadi raw body.
3. **No provider-specific code inference**. Gemini `status: "INVALID_ARGUMENT"` tidak diterjemahkan ke OpenAI `code`.
4. **Response duplication**. Error harus dibaca, diparse, lalu dibangun ulang menjadi `Response`/SSE chunk.

### How AxonRouter will be better
- **Typed error from the executor layer**: `BaseExecutor` langsung mengembalikan `*UpstreamError{StatusCode, Body, RawBody}` tanpa string wrapping. Tidak perlu regex reverse-parsing error message.
- **Per-provider byte-level translators**: pakai `gjson`/`sjson` di `[]byte`, tanpa convert ke string besar-besaran.
- **Single pass**: translate hanya sekali di executor; handler cuma forward.
- **No heavy sanitization**: message tetap di-truncate, tapi tidak pakai regex stack-trace tokenisasi yang mahal.

## Target Architecture

```
Provider upstream 4xx response
       │
   BaseExecutor.DoRequest / DoStreamRequestWithConfig
       │ returns *UpstreamError{StatusCode, RawBody}
       │
   Executor.Translate (per-provider)
       │ returns []byte OpenAI-compatible body → set as UpstreamError.Body
       │
   Handler.writeUpstreamClientError
       │ writes status + body directly
       ▼
Client receives HTTP 400 + {"error":{"message":"...","type":"invalid_request_error","code":"context_length_exceeded"}}
```

### Files to introduce
- `internal/executor/upstream_error.go` — pindahkan `UpstreamError` dari `cloudflare_error.go` ke file tersendiri.
- `internal/executor/translator/error.go` — interface `ErrorTranslator` + registry global.
- `internal/executor/translator/providers/{cloudflare,claude,gemini,antigravity,codex,kiro,openai}.go` — translator per provider.

### Files to modify
- `internal/executor/base.go` — return `*UpstreamError` untuk status >=400.
- `internal/executor/cloudflare.go` — pakai translator registry, hapus import `toCloudflareUpstreamError` hardcoded.
- `internal/executor/claude.go`, `gemini.go`, `antigravity.go`, `codex.go`, `kiro.go`, `openai.go` — panggil translator.
- `internal/executor/registry.go` — register translator saat register executor.

### Handler behavior
- `writeUpstreamClientError()` sudah cukup generic; hanya perlu handle 429 exhaustion agar message upstream tetap terlihat saat semua akun habis.

## Provider Translation Matrix

| Provider(s) | Executor | Upstream Error Format | Translation Needed | Priority |
|---|---|---|---|---|
| `cf` | `CloudflareExecutor` | `{"errors":[{"message":"AiError: AiError: {...}"}]}` | Anthropic-style nested JSON | Done (refactor to registry) |
| `claude`, `zai` | `ClaudeExecutor` | `{"type":"error","error":{"type":"...","message":"..."}}` | OpenAI shape | High |
| `gemini` | `GeminiExecutor` | `{"error":{"code":400,"message":"...","status":"INVALID_ARGUMENT"}}` | OpenAI shape + map `status` → `code` | High |
| `ag` | `AntigravityExecutor` | proprietary envelope | OpenAI shape + code inference | High |
| `cx` | `CodexExecutor` | likely OpenAI-Responses error | verify & passthrough | Medium |
| `kiro` | `KiroExecutor` | likely OpenAI-compatible | verify & passthrough | Medium |
| `openai`, `groq`, `deepseek`, `openrouter`, `oc`, `oc-zen`, `oc-go`, `mimocode`, `mimo-tp` | `OpenAIExecutor` | OpenAI-compatible | passthrough status/body as-is | Low |
| `elevenlabs`, `deepgram` | `OpenAIExecutor` | audio provider errors | passthrough for now | Low |

## Known Upstream Error Shapes

### Anthropic / Claude
```json
{"type":"error","error":{"type":"invalid_request_error","message":"..."}}
```
Mapping: `type` → `type`, `message` → `message`, `code` fallback dari tabel status.

### Gemini
```json
{"error":{"code":400,"message":"...","status":"INVALID_ARGUMENT"}}
```
Mapping: `status` → `code`, `message` → `message`, `type` dari HTTP status (400→invalid_request_error).

### Antigravity
Perlu capture sample dari response 400/401/429 beneran. Saat ini hanya tahu error dimasukkan sebagai string literal.

### Cloudflare (already handled)
```json
{"errors":[{"message":"AiError: AiError: {\"object\":\"error\",\"message\":\"...\",\"type\":\"BadRequestError\",\"param\":null,\"code\":400} (uuid)","code":8007}],"success":false,"result":{},"messages":[]}
```
Ekstrak nested JSON dari `errors[0].message`

## Implementation Phases

### Phase A — Foundation (semua phase lain tergantung ini)
- [ ] A1. Move `UpstreamError` dari `cloudflare_error.go` ke `internal/executor/upstream_error.go`.
- [ ] A2. Ubah `BaseExecutor.DoRequest` dan `DoStreamRequestWithConfig` supaya mengembalikan `*UpstreamError` untuk status >=400, bukan `fmt.Errorf(...)`.
- [ ] A3. Buat `internal/executor/translator/error.go`: interface `ErrorTranslator`, registry `Register`, `Translate`, default no-op translator.
- [ ] A4. Update semua executor yang sekarang membandingkan error text/test supaya tetap pass.
- [ ] A5. Update `connstate.DetectError` supaya bisa membaca `*UpstreamError.StatusCode`/body untuk klasifikasi yang lebih akurat.

### Phase B — Per-Provider Translators
- [ ] B1. Implementasi & test translator Claude/Zai.
- [ ] B2. Implementasi & test translator Gemini.
- [ ] B3. Implementasi & test translator Antigravity (butuh capture sample).
- [ ] B4. Verifikasi dan passthrough translator Codex.
- [ ] B5. Verifikasi dan passthrough translator Kiro.

### Phase C — OpenAI-Compatible Providers
- [ ] C1. Translator default untuk provider OpenAI-format hanya mem-forward body asli ke `UpstreamError.Body`.
- [ ] C2. Tambahkan ekstraksi `Retry-After` dari header/body untuk upstream 429 (opsional tapi direkomendasikan).

### Phase D — Hook Executor ke Translator
- [ ] D1. Register translator saat `RegisterDefaults()` di `internal/executor/registry.go`.
- [ ] D2. Update Cloudflare executor pakai translator registry (hapus hardcoded `toCloudflareUpstreamError`).
- [ ] D3. Update Claude, Gemini, Antigravity, Codex, Kiro, OpenAI executor untuk panggil translator setelah request.

### Phase E — Handler & Fallback Polish
- [ ] E1. Pastikan `writeUpstreamClientError` menulis 400/401/403/404/parsed error dengan cepat.
- [ ] E2. Jika semua akun habis karena 429, response final harus tetap mengandung upstream message (jangan generic 503).
- [ ] E3. Stream error event harus tetap OpenAI-compatible.

### Phase F — Validation
- [ ] F1. Unit tests untuk setiap translator dengan sample response asli.
- [ ] F2. Integration test handler: upstream 400 → client 400 OpenAI-compatible.
- [ ] F3. `go test ./...` pass.
- [ ] F4. `go vet ./...` clean.
- [ ] F5. `npm run build` zero warnings (bila ada perubahan frontend).

## Acceptance Criteria
1. Setiap provider prefix yang di-mount di `internal/executor/registry.go` menghasilkan OpenAI-compatible error body saat upstream mengembalikan 4xx/5xx.
2. `400 context_length_exceeded` dari Claude/Gemini/Cloudflare langsung terlihat oleh AI agent.
3. `429 rate_limit_exceeded` tetap memicu failover ke akun lain hingga semua akun exhaust.
4. Tidak ada lagi response `{"error":{"message":"all connections exhausted..."}}` untuk client-error (400).
5. Semua translator punya regression test dengan sample real response.
6. Lebih efisien dari OmniRoute: tidak ada `response.text()` async duplicated, tidak ada regex tokenisasi error string, tidak ada reverse parsing.

## Reference Code

### AxonRouter-GO (current)
- `internal/executor/cloudflare_error.go`
- `internal/executor/cloudflare.go`
- `internal/executor/base.go:417-423` (`DoStreamRequestWithConfig` raw error)
- `internal/api/handlers/v1/handler.go:668-702` (`writeUpstreamClientError`)
- `internal/executor/registry.go:96-123` (provider registration)

### OmniRoute (reference)
- `/workspaces/OmniRoute/open-sse/utils/error.ts` (`parseUpstreamError`, `buildErrorBody`, `sanitizeErrorMessage`)
- `/workspaces/OmniRoute/open-sse/config/errorConfig.ts` (`ERROR_TYPES`, `ERROR_RULES`, `COOLDOWN_MS`)
- `/workspaces/OmniRoute/open-sse/handlers/chatCore.ts:3160-3270` (error parsing → classification → cooldown)
