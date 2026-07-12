package openai

import (
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func stripData(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "data:") {
		s = strings.TrimSpace(s[5:])
	}
	return s
}

func TestStreamUsageInMessageDelta(t *testing.T) {
	var param any
	ctx := context.Background()

	chunks := []string{
		`data: {"id":"chatcmpl-1","model":"gpt-4o-mini","choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
		`data: {"id":"chatcmpl-1","model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":"hello"}}]}`,
		`data: {"id":"chatcmpl-1","model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":4}}}`,
		`data: [DONE]`,
	}

	var lastDelta string
	for _, c := range chunks {
		out := ConvertOpenAIResponseToClaudeStream(ctx, "", nil, nil, []byte(c), &param)
		for _, b := range out {
			s := string(b)
			if strings.Contains(s, `"type":"message_delta"`) {
				lastDelta = stripData(s)
			}
		}
	}

	if lastDelta == "" {
		t.Fatal("no message_delta event emitted")
	}
	rd := gjson.Parse(lastDelta)
	if rd.Get("delta.stop_reason").String() != "end_turn" {
		t.Errorf("stop_reason = %q, want end_turn", rd.Get("delta.stop_reason").String())
	}
	usage := rd.Get("delta.usage")
	if usage.Get("input_tokens").Int() != 10 {
		t.Errorf("input_tokens = %d, want 10", usage.Get("input_tokens").Int())
	}
	if usage.Get("output_tokens").Int() != 3 {
		t.Errorf("output_tokens = %d, want 3", usage.Get("output_tokens").Int())
	}
	if usage.Get("cache_read_input_tokens").Int() != 4 {
		t.Errorf("cache_read_input_tokens = %d, want 4", usage.Get("cache_read_input_tokens").Int())
	}
}

func TestStreamNoUsageWhenAbsent(t *testing.T) {
	var param any
	ctx := context.Background()
	chunks := []string{
		`data: {"id":"c","model":"m","choices":[{"index":0,"delta":{"content":"hi"}}]}`,
		`data: [DONE]`,
	}
	var lastDelta string
	for _, c := range chunks {
		for _, b := range ConvertOpenAIResponseToClaudeStream(ctx, "", nil, nil, []byte(c), &param) {
			s := string(b)
			if strings.Contains(s, `"type":"message_delta"`) {
				lastDelta = stripData(s)
			}
		}
	}
	usage := gjson.Parse(lastDelta).Get("delta.usage")
	if usage.Get("input_tokens").Int() != 0 || usage.Get("output_tokens").Int() != 0 {
		t.Errorf("expected zeroed usage, got %s", usage.Raw)
	}
	if usage.Get("cache_read_input_tokens").Exists() {
		t.Error("cache_read_input_tokens should be absent when cached_tokens is 0")
	}
}
