# Translation Gap Audit

Compared against `/workspaces/9router-mibp-version/open-sse/translator`.

## Verified parity

- OpenAI tool-call IDs and missing tool responses are normalized before routing in `internal/api/handlers/v1/{chat,messages,responses}.go`.
- Freebuff request metadata, `end_turn`, base3 root agents, stream filtering, empty `delta.tool_calls` removal, retry/session handling, and chat headers are aligned with `open-sse/executors/freebuff.js`.
- Go has additional native format pairs for Devin CLI, Qoder, and Gemini Interactions.

## Residual gaps

| Severity | Reference behavior | AxonRouter-Go behavior | Evidence |
| --- | --- | --- | --- |
| High | Dedicated `openai -> cursor` and `cursor -> openai` translators convert Cursor messages/tool results. | `cursor` is registered as generic `FormatOpenAI`; no Cursor translator or Cursor executor exists. | `open-sse/translator/request/openai-to-cursor.js`, `open-sse/translator/response/cursor-to-openai.js`, `internal/executor/registry.go:127` |
| High | Dedicated `openai <-> ollama` translators normalize Ollama message/tool-call shapes. | No Ollama format or translator is registered. | `open-sse/translator/request/openai-to-ollama.js`, `open-sse/translator/response/ollama-to-openai.js`, `internal/translator/types/types.go` |
| Medium | Dedicated `openai <-> vertex` translators map OpenAI content/tool/thinking fields to Vertex format. | `vertex` uses the generic OpenAI provider format; no Vertex translator pair exists. | `open-sse/translator/request/openai-to-vertex.js`, `open-sse/translator/response/gemini-to-openai.js`, `internal/executor/registry.go:136` |
| Medium | Dedicated `openai <-> commandcode` translators serialize CommandCode tool events. | CommandCode has a custom executor, but no format pair in the translator registry. | `open-sse/translator/request/openai-to-commandcode.js`, `open-sse/translator/response/commandcode-to-openai.js`, `internal/translator/types/types.go` |
| Medium | `gemini-cli` is a distinct format and is wrapped in a Cloud Code envelope. | No `FormatGeminiCLI`; Gemini CLI-specific translation is absent. | `open-sse/translator/request/openai-to-gemini.js`, `internal/translator/types/types.go` |
| Low | Translation entrypoint applies capability-driven content stripping, thinking normalization, session capture, and optional Claude tool cloaking globally. | These are distributed across handlers/provider adapters; there is no equivalent global invariant. | `open-sse/translator/index.js:35-57`, `internal/translator/registry/registry.go:58-77` |

## Scope note

The residual rows are provider/format support gaps, not Freebuff XML filtering gaps. Implementing them requires provider-specific wire contracts and tests; blindly pivoting them through generic OpenAI would be incorrect.
