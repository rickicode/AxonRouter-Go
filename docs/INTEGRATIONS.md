# AxonRouter-Go CLI Integrations

Point your coding agent at AxonRouter-Go's single `http://localhost:3777/v1` endpoint, set the model to any `provider/model` you configured, and keep coding.

## Claude Code

```bash
claude config set apiBaseUrl http://localhost:3777
claude config set model claude/claude-opus-4
```

You can override per request:

```bash
claude --model smart/balanced "refactor this file"
```

## Codex CLI

```bash
codex config set OPENAI_BASE_URL http://localhost:3777/v1
codex config set model cx/gpt-5.4
```

Codex Responses are translated automatically.

## Cursor

Open **Cursor Settings → Models**, add a custom OpenAI-compatible provider:

- Base URL: `http://localhost:3777/v1`
- API key: your AxonRouter API key

Use model names like `openai/gpt-4o`, `claude/claude-sonnet-4`, or combos like `smart/economy`.

## Cline / Roo Code

In `cline_custom_modes.json` or settings, set:

```json
{
  "openAiApiKey": "YOUR_AXON_KEY",
  "apiModelId": "smart/balanced",
  "openAiBaseUrl": "http://localhost:3777/v1"
}
```

## Continue

Add to `~/.continue/config.json`:

```json
{
  "models": [
    {
      "title": "AxonRouter",
      "provider": "openai",
      "model": "claude/claude-sonnet-4",
      "apiKey": "YOUR_AXON_KEY",
      "apiBase": "http://localhost:3777/v1"
    }
  ]
}
```

## OpenCode

Set in your OpenCode project config:

```json
{
  "model": "oc/qwen-coder-plus",
  "baseUrl": "http://localhost:3777/v1",
  "apiKey": "YOUR_AXON_KEY"
}
```

## Kiro

Kiro is OAuth-managed by AxonRouter. Add a Kiro connection in the dashboard, then use:

```bash
kiro --model kiro/claude-sonnet-4-20250514
```

## Any OpenAI-compatible tool

AxonRouter speaks OpenAI Chat Completions on `/v1/chat/completions`. Any tool that lets you set a base URL and API key works out of the box. Use provider-prefixed model names so AxonRouter knows where to route.
