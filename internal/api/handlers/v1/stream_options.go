package v1

import (
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// sanitizeStreamOptions injects or strips stream_options.include_usage from the
// translated request body based on whether the conditions for receiving token
// usage in streaming responses are met.
//
// Injection occurs only when ALL of these conditions are true:
// - stream == true
// - providerFmt == FormatOpenAI
// - The request is a chat completion path:
//   - path ends with "/chat/completions",
//   - path is "/v1/unified" with mode "text", or
//   - path is "/v1/messages" coming from a Claude client (translated to /chat/completions)
//
// In all other cases (non-streaming, non-OpenAI provider, Claude path without
// Claude client, or non-chat path), the entire stream_options object is removed
// from the body.
func sanitizeStreamOptions(body []byte, stream bool, clientFmt, providerFmt executor.ProviderFormat, path string) []byte {
	isChat := strings.HasSuffix(path, "/chat/completions")
	isUnifiedText := path == "/v1/unified" && gjson.GetBytes(body, "mode").String() == "text"
	isClaudeMessages := path == "/v1/messages" && clientFmt == executor.FormatClaude
	isEligible := (clientFmt == executor.FormatOpenAI && (isChat || isUnifiedText)) || isClaudeMessages
	if stream && providerFmt == executor.FormatOpenAI && isEligible {
		// Inject stream_options.include_usage so the OpenAI provider sends back
		// token usage in the final streaming chunk, enabling accurate cost tracking.
		out, err := sjson.SetBytes(body, "stream_options.include_usage", true)
		if err != nil {
			return body
		}
		return out
	}

	// Strip stream_options for all other cases — non-OpenAI providers don't
	// support it, and non-chat endpoints use different response formats.
	if gjson.GetBytes(body, "stream_options").Exists() {
		out, err := sjson.DeleteBytes(body, "stream_options")
		if err != nil {
			return body
		}
		return out
	}

	return body
}
