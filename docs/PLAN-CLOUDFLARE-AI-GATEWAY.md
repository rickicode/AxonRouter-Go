# Plan: Cloudflare Workers AI Gateway Provider

> Pattern: **match AMRouter `open-sse` 1:1** (provider `cloudflare-ai`, alias `cf`)

## Analysis

### API Reference (Cloudflare AI Gateway)

- **Base URL template**: `https://api.cloudflare.com/client/v4/accounts/{accountId}/ai/v1/chat/completions`
- **Auth**: `Authorization: Bearer {CLOUDFLARE_API_TOKEN}`
- **Format**: OpenAI-compatible
- **Model naming**: `@cf/{author}/{model}` for Workers AI
- **AMRouter provider id**: `cloudflare-ai`, alias `cf`

### AMRouter Pattern (source of truth)

1. **URL**: Template `{accountId}` in `baseUrl` → replaced from `providerSpecificData.accountId` at request time
2. **max_tokens cap**: 4096 (reasoning models), 8192 (normal) — CF free tier ~75s limit
3. **Content sanitization**: Strip non-`text`/`image_url` blocks — CF Zod validation is strict
4. **Error patterns**: `neurons`, `daily free allocation`, `upgrade to cloudflare`, `4006` → exhausted cooldown
5. **Model strip**: `["thinking"]` for reasoning models (deepseek-r1, kimi-k2.5, kimi-k2.6, glm-5.2, etc.)
6. **Input**: `email|accountid|apitoken` (email = name, accountid = PSD.account_id, apitoken = api_key)

---

## Implementation Plan

### Phase 1: Backend — Executor & Provider Registration

#### 1.1 `internal/executor/registry.go` — Register "cf" prefix

Add `"cf"` to OpenAI-compatible providers list in `RegisterDefaults()`:

```go
for _, p := range []string{"openai", "groq", "deepseek", ..., "cf"} {
```

#### 1.2 `internal/db/migrations.go` — Seed provider type with `{accountId}` template

```go
{"cf", "Cloudflare Workers AI", "openai", "https://api.cloudflare.com/client/v4/accounts/{accountId}/ai/v1/chat/completions"},
```

The `{accountId}` placeholder matches AMRouter pattern — resolved at request time from PSD.

### Phase 2: Backend — URL Template Resolution (match AMRouter `buildUrl`)

#### 2.1 `internal/executor/openai.go` — Add `{accountId}` replacement in `openAIEndpoint`

Modify `openAIEndpoint` to accept PSD and replace `{accountId}` (matching AMRouter `default.js:142-149`):

```go
func openAIEndpoint(baseURL, endpoint string, psd map[string]string) string {
    // Replace {accountId} template (Cloudflare Workers AI pattern)
    // Matches AMRouter DefaultExecutor.buildUrl() default case
    if strings.Contains(baseURL, "{accountId}") {
        if accountID, ok := psd["accountId"]; ok && accountID != "" {
            baseURL = strings.ReplaceAll(baseURL, "{accountId}", accountID)
        }
    }
    if baseURL == "" {
        return "https://api.openai.com/v1/" + endpoint
    }
    url := strings.TrimRight(baseURL, "/")
    for _, suffix := range []string{"/chat/completions", "/responses", "/embeddings", "/models"} {
        if strings.HasSuffix(url, suffix) {
            return url
        }
    }
    return url + "/" + endpoint
}
```

Update all callers to pass `req.ProviderSpecificData`:
- `Execute()`: `url := openAIEndpoint(req.BaseURL, "chat/completions", req.ProviderSpecificData)`
- `ExecuteStream()`: same
- `Embeddings()`: `url := openAIEndpoint(req.BaseURL, "embeddings", req.ProviderSpecificData)`
- `Models()`: `url := openAIEndpoint(req.BaseURL, "models", req.ProviderSpecificData)`
- `Responses()`: `url := openAIEndpoint(req.BaseURL, "responses", req.ProviderSpecificData)`
- `ResponsesStream()`: same

This is generic — any provider with `{accountId}` in its base_url template works automatically.

### Phase 3: Backend — Content Sanitization & max_tokens Cap (match AMRouter `chatCore.js:144-203`)

#### 3.1 `internal/executor/openai.go` — Add CF request sanitization

Add before Execute/ExecuteStream for "cf" provider (matching AMRouter chatCore.js lines 144-203):

```go
func sanitizeCFRequest(body []byte) []byte {
    // Cap max_tokens: 4096 for reasoning models, 8192 for normal
    // CF free tier has ~75s execution limit
    model := JSONGet(body, "model")
    isReasoning := strings.Contains(model, "r1") || strings.Contains(model, "qwq")
    cap := 8192
    if isReasoning {
        cap = 4096
    }
    // Parse current max_tokens, cap if needed
    // ... (read current value, if > cap or missing, set to cap)

    // Sanitize messages: CF Zod only allows {type:"text"} and {type:"image_url"}
    // Convert tool_result blocks to role:tool messages
    // Strip thinking/reasoning content blocks
    // ... (iterate messages, filter content blocks)

    return body
}
```

Call in `Execute()` and `ExecuteStream()` when `req.Provider == "cf"`:
```go
if req.Provider == "cf" {
    body = sanitizeCFRequest(body)
}
```

### Phase 4: Backend — Error Patterns (match AMRouter `errorConfig.js`)

#### 4.1 `internal/connstate/patterns.go` — Add CF error patterns

Add to error pattern matching (matching AMRouter errorConfig.js lines 68-71):

```go
// Cloudflare Workers AI error patterns (match AMRouter errorConfig.js)
{pattern: "neurons", category: ErrorQuotaExhausted},           // CF daily quota
{pattern: "daily free allocation", category: ErrorQuotaExhausted}, // CF daily quota
{pattern: "upgrade to cloudflare", category: ErrorQuotaExhausted}, // CF paid plan prompt
{pattern: "4006", category: ErrorQuotaExhausted},              // CF error code
```

### Phase 5: Backend — Accept PSD in AddConnection Handlers

#### 5.1 `internal/api/handlers/admin/providers.go` — `AddConnection`

Extend request struct:

```go
var req struct {
    Name                 string            `json:"name" binding:"required"`
    APIKey               string            `json:"api_key"`
    AuthType             string            `json:"auth_type"`
    Priority             int               `json:"priority"`
    ProviderSpecificData map[string]string `json:"provider_specific_data,omitempty"`
}
```

Store PSD as JSON in INSERT:
```go
var psdJSON sql.NullString
if len(req.ProviderSpecificData) > 0 {
    b, _ := json.Marshal(req.ProviderSpecificData)
    psdJSON = sql.NullString{String: string(b), Valid: true}
}
```

#### 5.2 `internal/api/handlers/admin/providers.go` — `BulkAddConnections`

Same PSD extension per connection.

### Phase 6: Frontend — Provider Catalog

#### 6.1 `web/src/lib/provider-catalog.ts` — Add Cloudflare entry

```typescript
{
  id: 'cf',
  displayName: 'Cloudflare Workers AI',
  icon: 'cloud',
  textIcon: 'CF',
  iconFile: '/providers/cloudflare.svg',
  category: 'apikey',
  description: 'Cloudflare Workers AI Gateway. OpenAI-compatible. Supports @cf/ models and third-party models via AI Gateway.',
  format: 'openai',
  authType: 'custom',
  prefix: 'cf/',
  isBuiltIn: true,
  website: 'https://developers.cloudflare.com/ai-gateway/',
  color: '#F38020',
  serviceKinds: ['llm'],
  hasFree: true,
  freeNote: 'Workers AI free tier: 10,000 neurons/day per account.',
  inputFormat: 'pipe',
},
```

Update `ProviderMeta` interface to include optional `inputFormat?: string`.

#### 6.2 `web/src/static/providers/` — Add Cloudflare icon

Add `cloudflare.svg` to `web/static/providers/`.

### Phase 7: Frontend — API Types & Modal

#### 7.1 `web/src/lib/api.ts` — Extend types

```typescript
export interface CreateConnectionPayload {
  name: string;
  auth_type?: 'api_key' | 'oauth' | 'none' | 'custom';
  api_key?: string;
  priority?: number;
  provider_specific_data?: Record<string, string>;
}
```

#### 7.2 `web/src/lib/components/AddConnectionModal.svelte` — Custom pipe parsing

**Bulk mode**: Parse `email|accountid|apitoken` format:

```typescript
function parseBulkConnections() {
  if (meta?.inputFormat === 'pipe') {
    return bulkText.split('\n').map(line => line.trim()).filter(Boolean)
      .map((line, index) => {
        const parts = line.split('|').map(p => p.trim());
        if (parts.length < 3) return null;
        const [email, accountId, apiToken] = parts;
        return {
          name: email || defaultName(index + 1),
          api_key: apiToken,
          provider_specific_data: { accountId },
        };
      }).filter(Boolean);
  }
  // ... existing comma/tab parsing
}
```

**Single mode**: Show 3 input fields (Email, Account ID, API Token) when `inputFormat === 'pipe'`.

**Textarea placeholder**:
```
user@example.com|account_id_1|api_token_1
user2@example.com|account_id_2|api_token_2
```

### Phase 8: Backend — Model Catalog

#### 8.1 `internal/models/models.json` — Add CF models (match AMRouter `providerModels.js`)

```json
{
  "cf": [
    {"id": "@cf/meta/llama-3.2-1b-instruct", "display_name": "Llama 3.2 1B Instruct"},
    {"id": "@cf/meta/llama-3.2-3b-instruct", "display_name": "Llama 3.2 3B Instruct"},
    {"id": "@cf/meta/llama-3.1-8b-instruct-fp8-fast", "display_name": "Llama 3.1 8B FP8 Fast"},
    {"id": "@cf/meta/llama-3.1-70b-instruct-fp8-fast", "display_name": "Llama 3.1 70B FP8 Fast"},
    {"id": "@cf/meta/llama-3.3-70b-instruct-fp8-fast", "display_name": "Llama 3.3 70B FP8 Fast"},
    {"id": "@cf/mistralai/mistral-small-3.1-24b-instruct", "display_name": "Mistral Small 3.1 24B"},
    {"id": "@cf/deepseek-ai/deepseek-r1-distill-qwen-32b", "display_name": "DeepSeek R1 Qwen 32B"},
    {"id": "@cf/moonshotai/kimi-k2.5", "display_name": "Kimi K2.5"},
    {"id": "@cf/moonshotai/kimi-k2.6", "display_name": "Kimi K2.6"},
    {"id": "@cf/zai-org/glm-5.2", "display_name": "GLM 5.2"},
    {"id": "@cf/qwen/qwq-32b", "display_name": "QwQ 32B"},
    {"id": "@cf/qwen/qwen2.5-coder-32b-instruct", "display_name": "Qwen 2.5 Coder 32B"}
  ]
}
```

### Phase 9: Verify

1. `go build ./cmd/server` — zero errors
2. `cd web && npm run build` — zero warnings
3. Smoke test:
   ```bash
   # Add CF connection
   curl -X POST localhost:3777/api/admin/providers/cf/connections \
     -H 'Content-Type: application/json' \
     -d '{"name":"test","api_key":"cf-token","provider_specific_data":{"accountId":"abc123"}}'

   # Test proxy
   curl -X POST localhost:3777/v1/chat/completions \
     -H 'Authorization: Bearer YOUR_PROXY_KEY' \
     -H 'Content-Type: application/json' \
     -d '{"model":"cf/@cf/meta/llama-3.1-8b-instruct-fp8-fast","messages":[{"role":"user","content":"hi"}]}'
   ```

---

## Files Changed (Summary)

| File | Change |
|------|--------|
| `internal/executor/registry.go` | Add "cf" to OpenAI-compatible list |
| `internal/executor/openai.go` | `{accountId}` template replacement + CF sanitization + max_tokens cap |
| `internal/db/migrations.go` | Add CF provider seed with `{accountId}` template URL |
| `internal/api/handlers/admin/providers.go` | Accept PSD in AddConnection/BulkAdd |
| `internal/connstate/patterns.go` | Add CF error patterns (neurons, daily free, etc.) |
| `internal/models/models.json` | Add CF model catalog |
| `web/src/lib/provider-catalog.ts` | Add CF entry + extend ProviderMeta |
| `web/src/lib/api.ts` | Extend CreateConnectionPayload |
| `web/src/lib/components/AddConnectionModal.svelte` | Custom pipe parsing + single mode fields |
| `web/static/providers/cloudflare.svg` | Icon (new) |
