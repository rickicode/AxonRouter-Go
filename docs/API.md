# AxonRouter-Go API Documentation

AxonRouter exposes an OpenAI-compatible proxy on `/v1/*` and an admin dashboard API on `/api/admin/*`.

- **User-facing overview:** [README.md](../README.md)
- **Deployment guide:** [DEPLOYMENT.md](./DEPLOYMENT.md)
- **Architecture:** [ARCHITECTURE.md](./ARCHITECTURE.md)

---

## Authentication

### Client API keys (`/v1/*`)

All requests to `/v1/*` proxy endpoints require a Bearer API key:

```bash
curl -H "Authorization: Bearer YOUR_API_KEY" \
  -X POST http://localhost:3777/v1/chat/completions \
  -d '{"model":"openai/gpt-4o","messages":[{"role":"user","content":"Hello"}]}'
```

API keys are managed in Dashboard → Settings or via the admin API.

### Admin session (`/api/admin/*`)

Admin endpoints require a JWT session:

1. `POST /api/admin/login` with `{"password": "..."}`.
2. The default password is randomly generated on first boot; change it with `axonrouter --setpass <password>`.
3. The response returns `{"token": "..."}` and sets `X-Auth-Token`.
4. Send the token as `Authorization: Bearer <token>` (or `X-Auth-Token`) on every `/api/admin/*` request.
5. Tokens are sliding: idle for 72 hours = logout.
6. `GET /api/admin/health` is public and returns version/health info.

---

## Proxy Endpoints

### POST /v1/chat/completions

OpenAI Chat Completions format.

**Request:**
```json
{
  "model": "openai/gpt-4o",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Hello!"}
  ],
  "stream": false,
  "temperature": 0.7
}
```

**Response:**
```json
{
  "id": "chatcmpl-xxx",
  "object": "chat.completion",
  "created": 1700000000,
  "model": "gpt-4o",
  "choices": [
    {
      "index": 0,
      "message": {"role": "assistant", "content": "Hello! How can I help?"},
      "finish_reason": "stop"
    }
  ],
  "usage": {"prompt_tokens": 10, "completion_tokens": 8, "total_tokens": 18}
}
```

**Streaming:**
```json
{
  "model": "openai/gpt-4o",
  "messages": [{"role": "user", "content": "Hello"}],
  "stream": true
}
```

Response: SSE stream dengan `data: {...}` chunks, diakhiri `data: [DONE]`

---

### POST /v1/messages

Anthropic Claude Messages format.

**Request:**
```json
{
  "model": "claude/claude-sonnet-4-20250514",
  "max_tokens": 1024,
  "messages": [
    {"role": "user", "content": "Hello!"}
  ]
}
```

**Response:**
```json
{
  "id": "msg_xxx",
  "type": "message",
  "role": "assistant",
  "content": [{"type": "text", "text": "Hello! How can I help?"}],
  "model": "claude-sonnet-4-20250514",
  "stop_reason": "end_turn",
  "usage": {"input_tokens": 10, "output_tokens": 8}
}
```

---

### POST /v1/responses

OpenAI Responses format (Codex). Mounted and reachable at `/v1/responses`; Codex-style requests can also be sent through `/v1/chat/completions`.

**Request:**
```json
{
  "model": "cx/gpt-5.4",
  "input": "Hello!",
  "stream": false
}
```

---

### GET /v1/models

Returns semua available models termasuk combo names dan virtual models.

**Response:**
```json
{
  "object": "list",
  "data": [
    {"id": "openai/gpt-4o", "object": "model", "created": 1700000000, "owned_by": "openai"},
    {"id": "claude/claude-sonnet-4", "object": "model", "created": 1700000000, "owned_by": "anthropic"},
    {"id": "balanced", "object": "model", "created": 1700000000, "owned_by": "axonrouter"},
    {"id": "smart/auto", "object": "model", "created": 1700000000, "owned_by": "axonrouter"},
    {"id": "smart/economy", "object": "model", "created": 1700000000, "owned_by": "axonrouter"},
    {"id": "smart/balanced", "object": "model", "created": 1700000000, "owned_by": "axonrouter"},
    {"id": "smart/premium", "object": "model", "created": 1700000000, "owned_by": "axonrouter"}
  ]
}
```

---

### POST /v1/messages/count_tokens

Count tokens untuk Claude models.

**Request:**
```json
{
  "model": "claude/claude-sonnet-4-20250514",
  "messages": [{"role": "user", "content": "Hello!"}]
}
```

**Response:**
```json
{
  "input_tokens": 10
}
```

---

### POST /v1/embeddings

OpenAI Embeddings format.

**Request:**
```json
{
  "model": "openai/text-embedding-3-small",
  "input": "Hello world"
}
```

---

### POST /v1/audio/speech

Text-to-speech.

**Request:**
```json
{
  "model": "openai/tts-1",
  "input": "Hello world",
  "voice": "alloy"
}
```

---

### POST /v1/audio/transcriptions

Speech-to-text.

**Request:** `multipart/form-data` dengan `file` field.

---

### POST /v1/images/generations

Image generation.

**Request:**
```json
{
  "model": "openai/dall-e-3",
  "prompt": "A cute cat",
  "n": 1,
  "size": "1024x1024"
}
```

---

### POST /v1/video/generations

Video generation.

**Request:**
```json
{
  "model": "openai/gpt-5.4-video",
  "prompt": "A futuristic cityscape at sunset"
}
```

---

### POST /v1/unified

Unified multi-modality gateway.

**Request:**
```json
{
  "mode": "text",
  "model": "openai/gpt-4o",
  "messages": [{"role": "user", "content": "Hello"}]
}
```

Modes: `text`, `image`, `audio`, `video`

---

## Admin API

### POST /api/admin/login

Issue a session JWT.

**Request:** `{"password": "string"}`

**Response (200):** `{"token": "eyJ..."}`

See the [Authentication](#authentication) section above for how to use the token.

### GET /api/admin/health

Public health check. Returns status, version, `latest_version`, `update_available`, and `must_change_password`.

### Providers

#### GET /api/admin/providers

List semua providers dengan connection counts.

**Response:**
```json
{
  "data": [
    {
      "id": "openai",
      "display_name": "openai",
      "format": "openai",
      "base_url": "https://api.openai.com/v1",
      "is_custom": false,
      "connection_count": 5,
      "status_counts": {"ready": 4, "rate_limited": 1}
    }
  ]
}
```

#### GET /api/admin/providers/:id

Provider detail dengan status breakdown.

#### POST /api/admin/providers

Add custom provider.

**Request:**
```json
{
  "name": "my-proxy",
  "format": "openai",
  "base_url": "https://my-proxy.example.com/v1",
  "custom_headers": {"X-Custom": "value"}
}
```

#### POST /api/admin/providers/:id/test

Test all connections untuk provider. Returns per-connection test results.

**Response:**
```json
{
  "provider_id": "openai",
  "results": [
    {"connection_id": "conn-1", "status": "ok", "latency_ms": 342},
    {"connection_id": "conn-2", "status": "failed", "error": "401 Unauthorized", "latency_ms": 120}
  ]
}
```

#### POST /api/admin/providers/:id/connections

Add connection ke provider.

**Request:**
```json
{
  "name": "my-key-001",
  "api_key": "sk-xxx",
  "auth_type": "api_key"
}
```

#### POST /api/admin/providers/:id/connections/bulk

Bulk add connections.

**Request:**
```json
{
  "connections": [
    {"name": "key-001", "api_key": "sk-xxx"},
    {"name": "key-002", "api_key": "sk-yyy"}
  ]
}
```

---

### Connections

#### GET /api/admin/providers/:id/connections

List connections untuk provider (paginated).

**Query params:** `page`, `per_page`, `status`, `search`

#### GET /api/admin/connections/:id

Connection detail.

#### PATCH /api/admin/connections/:id

Update connection.

#### DELETE /api/admin/connections/:id

Delete connection.

#### POST /api/admin/connections/:id/test

Test single connection.

**Response:**
```json
{
  "connection_id": "conn-1",
  "status": "ok",
  "status_code": 200,
  "latency_ms": 342
}
```

#### POST /api/admin/connections/:id/reset

Reset connection status ke `ready` dan sync in-memory state.

---

### Combos

#### GET /api/admin/combos

List semua combos.

#### POST /api/admin/combos

Create combo.

**Request:**
```json
{
  "name": "balanced",
  "strategy": "priority",
  "timeout_ms": 30000,
  "steps": [
    {"model_id": "mimo/mimo-v2-pro", "priority": 1},
    {"model_id": "cx/gpt-5.4", "priority": 2}
  ]
}
```

#### PATCH /api/admin/combos/:id

Update combo.

#### DELETE /api/admin/combos/:id

Delete combo.

#### POST /api/admin/combos/:id/steps

Add step ke combo.

#### DELETE /api/admin/combos/steps/:stepId

Remove step dari combo.

---

### Logs

#### GET /api/admin/logs

Request logs (paginated).

**Query params:** `page`, `per_page`, `provider`, `model`, `status`, `date_from`, `date_to`

**Response:**
```json
{
  "data": [
    {
      "id": "log-1",
      "connection_id": "conn-1",
      "provider_type_id": "openai",
      "model_id": "gpt-4o",
      "modality": "chat",
      "input_tokens": 100,
      "output_tokens": 50,
      "latency_ms": 1200,
      "status_code": 200,
      "created_at": 1700000000
    }
  ],
  "pagination": {
    "page": 1,
    "per_page": 100,
    "total": 1234,
    "total_pages": 13
  }
}
```

#### GET /api/admin/logs/stats

Log statistics.

---

### Settings

#### GET /api/admin/settings

List semua settings.

#### GET /api/admin/settings/:key

Get single setting.

#### PUT /api/admin/settings/:key

Update setting.

#### DELETE /api/admin/settings/:key

Delete setting.

---

### Dashboard

#### GET /api/admin/dashboard/stats

Dashboard statistics.

#### GET /api/admin/dashboard/providers

Provider summary.

#### GET /api/admin/dashboard/recent-logs

Recent logs.

---

### Models

| Endpoint | Description |
|----------|-------------|
| `GET /api/admin/providers/:id/models` | List models for a provider. |
| `POST /api/admin/providers/:id/models/test` | Test a model via this provider. |
| `POST /api/admin/models/sync` | Sync the model catalog. |

---

### OAuth

| Endpoint | Description |
|----------|-------------|
| `POST /api/admin/oauth/start` | Start an OAuth flow (PKCE / device code / Google / AWS). |
| `GET /api/admin/oauth/:sessionId/poll` | Poll OAuth status. |
| `POST /api/admin/oauth/callback` | Submit OAuth callback code/state. |

---

### Quota

| Endpoint | Description |
|----------|-------------|
| `GET /api/admin/quota` | List quota cache entries. |
| `GET /api/admin/quota/summary` | Quota summary. |
| `POST /api/admin/quota/:connId/refresh` | Refresh a connection's quota. |

---

### Model Pricing

| Endpoint | Description |
|----------|-------------|
| `GET /api/admin/model-pricing` | List per-model cost rates. |
| `POST /api/admin/model-pricing` | Create pricing entry. |
| `PATCH /api/admin/model-pricing/:id` | Update pricing entry. |
| `DELETE /api/admin/model-pricing/:id` | Delete pricing entry. |

---

### Proxy Pools

| Endpoint | Description |
|----------|-------------|
| `GET /api/admin/proxy-pools` | List proxy pools. |
| `POST /api/admin/proxy-pools` | Create proxy pool. |
| `POST /api/admin/proxy-pools/bulk` | Bulk create proxy pools. |
| `GET /api/admin/proxy-pools/:id` | Proxy pool detail. |
| `PATCH /api/admin/proxy-pools/:id` | Update proxy pool. |
| `DELETE /api/admin/proxy-pools/:id` | Delete proxy pool. |
| `POST /api/admin/proxy-pools/:id/test` | Test proxy pool. |
| `POST /api/admin/proxy-pools/bulk-delete` | Bulk delete by ids or test_status. |
| `GET /api/admin/proxy-pools/health-check` | Get health-check status. |
| `POST /api/admin/proxy-pools/health-check` | Run health check. |
| `GET /api/admin/proxy-pools/generate-source` | Generate deploy source. |
| `POST /api/admin/proxy-pools/vercel-deploy` | Deploy to Vercel. |
| `POST /api/admin/proxy-pools/deno-deploy` | Deploy to Deno. |
| `POST /api/admin/proxy-pools/cloudflare-deploy` | Deploy to Cloudflare. |

---

### TLS Config

Native HTTPS on port 443 via Let's Encrypt. Configured from Dashboard → Settings → HTTPS.

| Endpoint | Description |
|----------|-------------|
| `GET /api/admin/tls-config` | Get current TLS configuration. |
| `GET /api/admin/tls-config/public-ip` | Detect public IP with `AXON_PUBLIC_IP` override. |
| `GET /api/admin/tls-config/check-dns` | Check DNS readiness for the configured domain. |

---

### Upgrade

| Endpoint | Description |
|----------|-------------|
| `POST /api/admin/upgrade` | Download the latest release binary for the current platform, verify SHA256 against `checksums.txt`, and write it to `<DataDir>/bin/<asset>`. |

---

## Error Responses

Semua error mengikuti format:

```json
{
  "error": {
    "message": "error description",
    "type": "error_type"
  }
}
```

Error types:
- `invalid_request_error` — Bad request
- `auth_error` — Authentication failed
- `server_error` — Internal server error
- `rate_limit_error` — Rate limit exceeded

---

## Rate Limiting

Rate limiting diaplikasikan per-API-key (jika `rate_limit` diset di api_keys table) atau per-IP (default).

**Response header:**
```
X-RateLimit-Limit: 600
X-RateLimit-Remaining: 599
X-RateLimit-Reset: 1700000060
```

**429 Response:**
```json
{
  "error": {
    "message": "rate limit exceeded",
    "type": "rate_limit_error"
  }
}
```
