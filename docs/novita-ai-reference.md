# Novita AI Reference

## Endpoints

| Endpoint | URL | Auth |
|----------|-----|------|
| API Base | `https://api.novita.ai` | Bearer token |
| OpenAI-Compatible | `https://api.novita.ai/openai` | Bearer token |
| Chat Completions | `https://api.novita.ai/v3/openai/chat/completions` | Bearer token |
| Chat Completions (alt) | `https://api.novita.ai/openai/chat/completions` | Bearer token |
| List Models | `https://api.novita.ai/v3/openai/models` | **Public (no auth)** |

## Authentication

```
Authorization: Bearer <API_KEY>
```

Get API key from: https://novita.ai/console

## Models

- 139 total models (as of 2026-07-12)
- No public `:free` models in API — free access is through partners like OpenCode

### Key Models

| Model | Architecture | Context | Notes |
|-------|-------------|---------|-------|
| `tencent/hy3` | 295B/21B MoE | 256K | Business-focused, coding, agentic |
| `tencent/hy3-20260706:free` | Same | 256K | Free tier (via OpenCode) |

## OpenCode Integration

OpenCode Free (`https://opencode.ai/zen/v1`) is a meta-router that uses Novita AI as a backend for some models:

- `oc/hy3-free` → routes to `tencent/hy3-20260706:free` on Novita
- `oc/deepseek-v4-flash-free` → routes to DeepSeek
- Other models route to their respective backends

The `"provider":"Novita"` in OpenCode's response indicates the internal backend, not the AxonRouter provider.

## Rate Limiting

- Free tier: rate-limited per account/IP
- `FreeUsageLimitError` (HTTP 429) when limit exceeded
- No `Retry-After` header provided

## Pricing

- Free to start (requires registration)
- Pay-as-you-go for heavy usage
- Docs: https://novita.ai/pricing
