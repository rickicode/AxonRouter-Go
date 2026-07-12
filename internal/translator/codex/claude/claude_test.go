package claude

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestCodexRequestToClaude(t *testing.T) {
	req := []byte(`{
		"model": "claude-opus-4-1",
		"instructions": "You are a helpful assistant.",
		"input": [
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
			{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"LA\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"sunny"}
		],
		"tools": [
			{"type":"function","name":"get_weather","description":"weather","parameters":{"type":"object","properties":{}}},
			{"type":"web_search"}
		],
		"tool_choice": {"type":"function","name":"get_weather"},
		"reasoning": {"effort":"medium"}
	}`)

	out := convertCodexRequestToClaude("claude-opus-4-1", req, true)
	root := gjson.ParseBytes(out)

	if root.Get("model").String() != "claude-opus-4-1" {
		t.Fatalf("model mismatch: %s", root.Get("model").String())
	}

	// system should combine instructions and input developer content.
	sys := root.Get("system")
	if !sys.IsArray() || sys.Get("0.text").String() != "You are a helpful assistant." {
		t.Fatalf("unexpected system: %s", sys.Raw)
	}

	msgs := root.Get("messages").Array()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[1].Get("role").String() != "assistant" {
		t.Fatalf("expected function_call -> assistant, got %s", msgs[1].Get("role").String())
	}
	if msgs[1].Get("content.0.input.city").String() != "LA" {
		t.Fatalf("tool_use args not parsed: %s", msgs[1].Get("content.0.input").Raw)
	}

	// Tools should be flat for function tools, web_search_20250305 for built-in.
	tools := root.Get("tools").Array()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Get("name").String() != "get_weather" || tools[0].Get("input_schema.type").String() != "object" {
		t.Fatalf("unexpected function tool shape: %s", tools[0].Raw)
	}
	if tools[1].Get("type").String() != "web_search_20250305" {
		t.Fatalf("unexpected web search tool type: %s", tools[1].Get("type").String())
	}

	// tool_choice should map to Claude tool object.
	if root.Get("tool_choice.type").String() != "tool" || root.Get("tool_choice.name").String() != "get_weather" {
		t.Fatalf("unexpected tool_choice: %s", root.Get("tool_choice").Raw)
	}

	// reasoning.effort medium -> thinking budget 8192
	if root.Get("thinking.budget_tokens").Int() != 8192 {
		t.Fatalf("unexpected thinking budget: %s", root.Get("thinking").Raw)
	}
}

func TestClaudeStreamToCodexResponse(t *testing.T) {
	var state any
	var acc []byte

	chunks := []string{
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-4-1","created_at":1700000000}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":10,"output_tokens":2}}`,
		`{"type":"message_stop"}`,
	}

	for _, c := range chunks {
		out := convertClaudeResponseToCodexStream(context.Background(), "", nil, nil, []byte("data: "+c+"\n\n"), &state)
		if len(out) > 0 {
			acc = append(acc, out[0]...)
		}
	}

	if !bytes.Contains(acc, []byte("response.created")) {
		t.Fatalf("missing response.created in output:\n%s", string(acc))
	}
	if !bytes.Contains(acc, []byte("response.output_text.delta")) {
		t.Fatalf("missing response.output_text.delta in output:\n%s", string(acc))
	}
	if !bytes.Contains(acc, []byte("response.completed")) {
		t.Fatalf("missing response.completed in output:\n%s", string(acc))
	}
	if !bytes.HasPrefix(acc, []byte("data: ")) {
		t.Fatalf("output not SSE formatted:\n%s", string(acc))
	}
}

func TestClaudeNonStreamToCodexResponse(t *testing.T) {
	claudeResp := []byte(`{
		"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-4-1",
		"content":[
			{"type":"text","text":"The answer is "},
			{"type":"text","text":"42."},
			{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"LA"}},
			{"type":"thinking","thinking":"I should think","signature":"abc123"}
		],
		"stop_reason":"tool_use",
		"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":2}
	}`)

	out := convertClaudeResponseToCodexNonStream(context.Background(), "", nil, nil, claudeResp, nil)
	root := gjson.ParseBytes(out)

	if root.Get("stop_reason").String() != "tool_use" {
		t.Fatalf("unexpected stop_reason: %s", root.Get("stop_reason").String())
	}
	if root.Get("usage.total_tokens").Int() != 15 {
		t.Fatalf("unexpected total_tokens: %d", root.Get("usage.total_tokens").Int())
	}
	if root.Get("usage.input_tokens_details.cached_tokens").Int() != 2 {
		t.Fatalf("missing cached tokens: %s", root.Get("usage").Raw)
	}

	output := root.Get("output").Array()
	if len(output) != 3 {
		t.Fatalf("expected 3 output items, got %d: %s", len(output), root.Get("output").Raw)
	}

	var foundText, foundTool, foundReasoning bool
	for _, item := range output {
		switch item.Get("type").String() {
		case "message":
			if strings.Contains(item.Get("content.0.text").String(), "42") {
				foundText = true
			}
		case "function_call":
			if item.Get("name").String() == "get_weather" {
				foundTool = true
			}
		case "reasoning":
			if item.Get("encrypted_content").String() == "abc123" {
				foundReasoning = true
			}
		}
	}
	if !foundText || !foundTool || !foundReasoning {
		t.Fatalf("missing output items: text=%v tool=%v reasoning=%v", foundText, foundTool, foundReasoning)
	}
}
