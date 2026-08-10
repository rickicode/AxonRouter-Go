package claude

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func sseLine(payload string) []byte {
	return []byte("data: " + payload)
}

func TestConvertClaudeResponseToOpenAIResponses_Stream(t *testing.T) {
	lines := [][]byte{
		sseLine(`{"type":"message_start","message":{"id":"msg_123","usage":{"input_tokens":10,"output_tokens":0}}}`),
		sseLine(`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`),
		sseLine(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello "}}`),
		sseLine(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world"}}`),
		sseLine(`{"type":"content_block_stop","index":0}`),
		sseLine(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_abc","name":"get_weather"}}`),
		sseLine(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"loc"}}`),
		sseLine(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"ation\":\"NYC\"}"}}`),
		sseLine(`{"type":"content_block_stop","index":1}`),
		sseLine(`{"type":"message_delta","usage":{"output_tokens":5}}`),
		sseLine(`{"type":"message_stop"}`),
	}

	var param any
	var events []string
	for _, line := range lines {
		for _, ev := range ConvertClaudeResponseToOpenAIResponses(context.Background(), "claude-opus-4", nil, nil, line, &param) {
			events = append(events, string(ev))
		}
	}

	joined := strings.Join(events, "")
	wantContains := []string{
		"event: response.created",
		"event: response.in_progress",
		"event: response.output_item.added",
		"event: response.content_part.added",
		"event: response.output_text.delta",
		"event: response.output_text.done",
		"event: response.content_part.done",
		"event: response.function_call_arguments.delta",
		"event: response.function_call_arguments.done",
		"event: response.output_item.done",
		"event: response.completed",
	}
	for _, want := range wantContains {
		if !strings.Contains(joined, want) {
			t.Errorf("missing event %q in stream output", want)
		}
	}

	if !strings.Contains(joined, `"type":"function_call"`) {
		t.Errorf("missing function_call item in output")
	}
	if !strings.Contains(joined, `"text":"Hello world"`) {
		t.Errorf("aggregated text missing in stream output")
	}

	// Each event must end with a blank line.
	for _, ev := range events {
		if !bytes.HasSuffix([]byte(ev), []byte("\n\n")) {
			t.Errorf("event missing trailing \\n\\n: %q", ev)
		}
	}
}

func TestConvertClaudeResponseToOpenAIResponses_WebSearchBlocks(t *testing.T) {
	lines := [][]byte{
		sseLine(`{"type":"message_start","message":{"id":"msg_123","usage":{"input_tokens":10,"output_tokens":0}}}`),
		sseLine(`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`),
		sseLine(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"**Compare competitors**\n- "}}`),
		sseLine(`{"type":"content_block_stop","index":0}`),
		sseLine(`{"type":"content_block_start","index":1,"content_block":{"type":"server_tool_use","id":"srv_123","name":"web_search","input":{}}}`),
		sseLine(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"Qwen3\"}"}}`),
		sseLine(`{"type":"content_block_stop","index":1}`),
		sseLine(`{"type":"content_block_start","index":2,"content_block":{"type":"web_search_tool_result","tool_use_id":"srv_123","content":[{"type":"web_search_result","title":"Example","url":"https://example.com"}]}}`),
		sseLine(`{"type":"content_block_stop","index":2}`),
		sseLine(`{"type":"content_block_delta","index":1,"delta":{"type":"citations_delta","citation":{"type":"web_search_result_location","cited_text":"Qwen 3.7 Max","url":"https://example.com","title":"Example"}}}`),
		sseLine(`{"type":"content_block_start","index":3,"content_block":{"type":"text"}}`),
		sseLine(`{"type":"content_block_delta","index":3,"delta":{"type":"text_delta","text":"Qwen 3.7 Max leads."}}`),
		sseLine(`{"type":"content_block_stop","index":3}`),
		sseLine(`{"type":"message_delta","usage":{"output_tokens":12}}`),
		sseLine(`{"type":"message_stop"}`),
	}

	var param any
	var events []string
	for _, line := range lines {
		for _, ev := range ConvertClaudeResponseToOpenAIResponses(context.Background(), "claude-opus-4", nil, nil, line, &param) {
			events = append(events, string(ev))
		}
	}

	joined := strings.Join(events, "")

	if strings.Contains(joined, `"type":"function_call"`) {
		t.Errorf("web-search server tool use should not emit a function_call item")
	}
	if !strings.Contains(joined, `"text":"**Compare competitors**\n- Qwen 3.7 Max leads."`) {
		t.Errorf("text before and after web-search block should aggregate into one message")
	}
	if !strings.Contains(joined, `"type":"web_search_result_location"`) {
		t.Errorf("web-search citation annotation missing")
	}
	if !strings.Contains(joined, `"annotations":[{`) {
		t.Errorf("citation annotation not attached to output text")
	}
}

func TestConvertClaudeResponseToOpenAIResponsesNonStream(t *testing.T) {
	var raw []byte
	for _, line := range []string{
		`{"type":"message_start","message":{"id":"msg_123","usage":{"input_tokens":10,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello "}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_abc","name":"get_weather"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"location\":\"NYC\"}"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","usage":{"output_tokens":5}}`,
		`{"type":"message_stop"}`,
	} {
		raw = append(raw, []byte("data: "+line+"\n")...)
	}

	out := ConvertClaudeResponseToOpenAIResponsesNonStream(context.Background(), "claude-opus-4", nil, nil, raw, nil)

	if got := gjson.GetBytes(out, "id").String(); got != "msg_123" {
		t.Errorf("id = %q, want msg_123", got)
	}
	if got := gjson.GetBytes(out, "status").String(); got != "completed" {
		t.Errorf("status = %q, want completed", got)
	}

	output := gjson.GetBytes(out, "output").Array()
	if len(output) != 2 {
		t.Fatalf("output length = %d, want 2", len(output))
	}
	if got := output[0].Get("type").String(); got != "message" {
		t.Errorf("output[0].type = %q, want message", got)
	}
	if got := output[0].Get("content.0.text").String(); got != "Hello world" {
		t.Errorf("output[0] text = %q, want 'Hello world'", got)
	}
	if got := output[1].Get("type").String(); got != "function_call" {
		t.Errorf("output[1].type = %q, want function_call", got)
	}
	if got := output[1].Get("call_id").String(); got != "toolu_abc" {
		t.Errorf("output[1].call_id = %q, want toolu_abc", got)
	}
	if got := output[1].Get("name").String(); got != "get_weather" {
		t.Errorf("output[1].name = %q, want get_weather", got)
	}
	wantArgs := `{"location":"NYC"}`
	if got := output[1].Get("arguments").String(); got != wantArgs {
		t.Errorf("output[1].arguments = %q, want %q", got, wantArgs)
	}

	if got := gjson.GetBytes(out, "usage.input_tokens").Int(); got != 10 {
		t.Errorf("usage.input_tokens = %d, want 10", got)
	}
	if got := gjson.GetBytes(out, "usage.output_tokens").Int(); got != 5 {
		t.Errorf("usage.output_tokens = %d, want 5", got)
	}
}
