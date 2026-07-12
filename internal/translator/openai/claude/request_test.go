package claude

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestOpenAIToolCallsBecomeToolUse(t *testing.T) {
	body := []byte(`{
		"model": "m",
		"messages": [
			{"role": "assistant", "content": null, "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "calc", "arguments": "{\"a\":1}"}}
			]}
		]
	}`)
	out := convertOpenAIRequestToClaude("m", body, false)
	root := gjson.ParseBytes(out)
	part := root.Get("messages.0.content.0")
	if part.Get("type").String() != "tool_use" {
		t.Fatalf("content part type = %q, want tool_use", part.Get("type").String())
	}
	if part.Get("id").String() != "call_1" {
		t.Errorf("id = %q, want call_1", part.Get("id").String())
	}
	if part.Get("name").String() != "calc_cc" {
		t.Errorf("name = %q, want calc_cc (cloaked)", part.Get("name").String())
	}
}
