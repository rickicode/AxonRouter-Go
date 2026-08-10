# CommandCode AI Provider

## Overview
AxonRouter-Go supports CommandCode AI as a built-in provider.

- **Provider ID:** `commandcode`
- **Alias:** `cmd`
- **Base URL:** `https://api.commandcode.ai`
- **Chat endpoint:** `POST https://api.commandcode.ai/alpha/generate`
- **Models endpoint:** `GET https://api.commandcode.ai/provider/v1/models`
- **Auth:** API key via `Authorization: Bearer <key>`

## Supported models
The following model IDs are seeded in the gateway catalog under the `commandcode/` prefix:

- `claude-opus-4-7`
- `claude-opus-4-6`
- `claude-sonnet-4-6`
- `claude-haiku-4-5-20251001`
- `gpt-5.5`
- `gpt-5.4`
- `gpt-5.3-codex`
- `gpt-5.4-mini`
- `deepseek/deepseek-v4-pro`
- `deepseek/deepseek-v4-flash`
- `moonshotai/Kimi-K2.6`
- `moonshotai/Kimi-K2.5`
- `zai-org/GLM-5.1`
- `zai-org/GLM-5`
- `MiniMaxAI/MiniMax-M2.7`
- `MiniMaxAI/MiniMax-M2.5`
- `Qwen/Qwen3.6-Max-Preview`
- `Qwen/Qwen3.6-Plus`

## Request normalization
The CommandCode executor applies the following transforms before forwarding:
- Merge `system` and `developer` messages into CommandCode's top-level `system` field.
- Strip the gateway `commandcode/` prefix from the model ID.
- Clamp a client-supplied positive `max_tokens` to 200,000 (the upstream ceiling) and omit non-positive values.
- Remove empty `tools` arrays.

## Usage example
```json
{
  "model": "commandcode/deepseek/deepseek-v4-pro",
  "messages": [
    {"role": "user", "content": "Hello!"}
  ]
}
```

## Dashboard
The Svelte provider catalog already includes CommandCode AI with the ID `commandcode`, alias `cmd`, and API-key auth. Add a connection with the provider type `commandcode` and an API key to start routing requests.
