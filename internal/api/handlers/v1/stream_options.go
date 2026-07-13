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
//   - stream == true
//   - clientFmt == FormatOpenAI
//   - providerFmt == FormatOpenAI
//   - path ends with "/chat/completions"
//
// In all other cases (non-streaming, non-OpenAI client, non-OpenAI provider,
// or non-chat path), the entire stream_options object is removed from the body.
func sanitizeStreamOptions(body []byte, stream bool, clientFmt, providerFmt executor.ProviderFormat, path string) []byte {
	if stream && clientFmt == executor.FormatOpenAI && providerFmt == executor.FormatOpenAI && strings.HasSuffix(path, "/chat/completions") {
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
