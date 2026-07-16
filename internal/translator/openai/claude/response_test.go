package claude

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

func TestClaudeNonStreamContextWindowExceeded(t *testing.T) {
	raw := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5","content":[{"type":"text","text":"ok"}],"stop_reason":"model_context_window_exceeded","usage":{"input_tokens":1,"output_tokens":1}}`
	got := convertClaudeResponseToOpenAINonStream(context.Background(), "", nil, nil, []byte(raw), nil)
	finish := gjson.GetBytes(got, "choices.0.finish_reason").String()
	if finish != "length" {
		t.Errorf("finish_reason=%q, want length", finish)
	}
}

func TestClaudeStreamContextWindowExceeded(t *testing.T) {
	var param any
	ctx := context.Background()
	chunks := []string{
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":0}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"model_context_window_exceeded"},"usage":{"input_tokens":1,"output_tokens":1}}`,
		`data: {"type":"message_stop"}`,
	}
	var sawLength bool
	for _, c := range chunks {
		for _, b := range convertClaudeResponseToOpenAIStream(ctx, "", nil, nil, []byte(c), &param) {
			s := stripData(string(b))
			if gjson.Get(s, "choices.0.finish_reason").String() == "length" {
				sawLength = true
			}
		}
	}
	if !sawLength {
		t.Error("expected finish_reason=length for model_context_window_exceeded")
	}
}

func TestClaudeStreamToolUseRoundTrip(t *testing.T) {
	var param any
	ctx := context.Background()
	chunks := []string{
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"calc","input":{}}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"a\":1}"}}`,
		`data: {"type":"content_block_stop","index":0}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"input_tokens":1,"output_tokens":5}}`,
		`data: {"type":"message_stop"}`,
	}
	var sawToolCall bool
	for _, c := range chunks {
		for _, b := range convertClaudeResponseToOpenAIStream(ctx, "", nil, nil, []byte(c), &param) {
			s := stripData(string(b))
			if strings.Contains(s, `"tool_calls"`) && strings.Contains(s, `"function"`) {
				sawToolCall = true
				if !gjson.Valid(s) {
					t.Errorf("invalid chunk JSON: %s", s)
				}
			}
		}
	}
	if !sawToolCall {
		t.Error("expected an OpenAI tool_calls delta for the tool_use block")
	}
}
