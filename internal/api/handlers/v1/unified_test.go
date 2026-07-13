package v1

import (
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/tidwall/gjson"
)

func TestUnifiedStreamUsage(t *testing.T) {
	t.Run("text mode streaming injects include_usage", func(t *testing.T) {
		body := []byte(`{"mode":"text","model":"openai/gpt-4","stream":true}`)
		result := sanitizeStreamOptions(body, true, executor.FormatOpenAI, executor.FormatOpenAI, "/v1/unified")
		if !gjson.GetBytes(result, "stream_options.include_usage").Bool() {
			t.Error("expected stream_options.include_usage injected for /v1/unified text mode")
		}
	})

	t.Run("non-text mode does not inject", func(t *testing.T) {
		body := []byte(`{"mode":"image","model":"openai/gpt-4","stream":true}`)
		result := sanitizeStreamOptions(body, true, executor.FormatOpenAI, executor.FormatOpenAI, "/v1/unified")
		if gjson.GetBytes(result, "stream_options").Exists() {
			t.Error("expected stream_options to be stripped for non-text /v1/unified mode")
		}
	})

	t.Run("non-streaming text mode does not inject", func(t *testing.T) {
		body := []byte(`{"mode":"text","model":"openai/gpt-4","stream":false}`)
		result := sanitizeStreamOptions(body, false, executor.FormatOpenAI, executor.FormatOpenAI, "/v1/unified")
		if gjson.GetBytes(result, "stream_options").Exists() {
			t.Error("expected stream_options to be stripped for non-streaming /v1/unified")
		}
	})
}
