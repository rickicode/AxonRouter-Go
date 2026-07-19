package grok_cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
	"github.com/tidwall/gjson"
)

func chunk(s string) []byte {
	return []byte("data: " + s + "\n\n")
}

func collectStream(t *testing.T, events ...string) [][]byte {
	t.Helper()
	var param any
	var out [][]byte
	for _, e := range events {
		chunks := convertGrokResponseToOpenAIStream(context.Background(), "grok-cli/grok-4.3", nil, nil, chunk(e), &param)
		out = append(out, chunks...)
	}
	return out
}

func TestStreamTextDelta(t *testing.T) {
	events := []string{
		`{"type":"response.created","response":{"id":"resp_1","model":"grok-4.3"}}`,
		`{"type":"response.output_text.delta","delta":"Hello"}`,
		`{"type":"response.completed"}`,
	}
	chunks := collectStream(t, events...)
	if len(chunks) != 2 { // text chunk + completed chunk; [DONE] included in completed
		t.Fatalf("expected 2 chunks, got %d: %s", len(chunks), stringify(chunks))
	}
	jsonPart := bytes.TrimSpace(chunks[0][5:])
	if got := gjson.GetBytes(jsonPart, "choices.0.delta.content").String(); got != "Hello" {
		t.Fatalf("expected delta content Hello, got %s", got)
	}
	completed := bytes.TrimSpace(chunks[1][5:])
	if got := gjson.GetBytes(completed, "choices.0.finish_reason").String(); got != "stop" {
		t.Fatalf("expected finish_reason stop, got %s", got)
	}
}

func TestStreamFunctionCall(t *testing.T) {
	events := []string{
		`{"type":"response.created","response":{"id":"resp_1","model":"grok-4.3"}}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"LA\"}"}}`,
		`{"type":"response.completed"}`,
	}
	chunks := collectStream(t, events...)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %s", len(chunks), stringify(chunks))
	}
	fc := bytes.TrimSpace(chunks[0][5:])
	if got := gjson.GetBytes(fc, "choices.0.delta.tool_calls.0.function.name").String(); got != "get_weather" {
		t.Fatalf("expected tool name get_weather, got %s", got)
	}
	if got := gjson.GetBytes(fc, "choices.0.delta.tool_calls.0.function.arguments").String(); got != `{"city":"LA"}` {
		t.Fatalf("expected args, got %s", got)
	}
	completed := bytes.TrimSpace(chunks[1][5:])
	if got := gjson.GetBytes(completed, "choices.0.finish_reason").String(); got != "tool_calls" {
		t.Fatalf("expected finish_reason tool_calls, got %s", got)
	}
}

func TestStreamIgnoresArgumentDeltas(t *testing.T) {
	events := []string{
		`{"type":"response.created","response":{"id":"resp_1"}}`,
		`{"type":"response.function_call_arguments.delta","delta":"{\"city\": \"L"}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"LA\"}"}}`,
		`{"type":"response.completed"}`,
	}
	chunks := collectStream(t, events...)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %s", len(chunks), stringify(chunks))
	}
}

func TestStreamAccumulatesFunctionArgumentsDelta(t *testing.T) {
	events := []string{
		`{"type":"response.created","response":{"id":"resp_1"}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"call_1","delta":"{\"city\":\""}`,
		`{"type":"response.function_call_arguments.delta","item_id":"call_1","delta":"LA"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"call_1","delta":"\"}"}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":""}}`,
		`{"type":"response.completed"}`,
	}
	chunks := collectStream(t, events...)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %s", len(chunks), stringify(chunks))
	}
	fc := bytes.TrimSpace(chunks[0][5:])
	if got := gjson.GetBytes(fc, "choices.0.delta.tool_calls.0.function.arguments").String(); got != `{"city":"LA"}` {
		t.Fatalf("expected accumulated args {\"city\":\"LA\"}, got %s", got)
	}
	completed := bytes.TrimSpace(chunks[1][5:])
	if got := gjson.GetBytes(completed, "choices.0.finish_reason").String(); got != "tool_calls" {
		t.Fatalf("expected finish_reason tool_calls, got %s", got)
	}
}

func TestNonStreamText(t *testing.T) {
	resp := []byte(`{"id":"resp_1","model":"grok-4.3","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Done"}]}],"usage":{"input_tokens":7,"output_tokens":3}}`)
	out := convertGrokResponseToOpenAINonStream(context.Background(), "grok-cli/grok-4.3", nil, nil, resp, nil)

	if got := gjson.GetBytes(out, "choices.0.message.content").String(); got != "Done" {
		t.Fatalf("expected content Done, got %s", got)
	}
	if got := gjson.GetBytes(out, "choices.0.finish_reason").String(); got != "stop" {
		t.Fatalf("expected stop, got %s", got)
	}
	if got := gjson.GetBytes(out, "usage.prompt_tokens").Int(); got != 7 {
		t.Fatalf("expected prompt_tokens 7, got %d", got)
	}
	if got := gjson.GetBytes(out, "usage.completion_tokens").Int(); got != 3 {
		t.Fatalf("expected completion_tokens 3, got %d", got)
	}
}

func TestNonStreamFunctionCall(t *testing.T) {
	resp := []byte(`{"id":"resp_2","model":"grok-4.3","output":[{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"LA\"}"}]}`)
	out := convertGrokResponseToOpenAINonStream(context.Background(), "grok-cli/grok-4.3", nil, nil, resp, nil)

	if got := gjson.GetBytes(out, "choices.0.finish_reason").String(); got != "tool_calls" {
		t.Fatalf("expected tool_calls, got %s", got)
	}
	if got := gjson.GetBytes(out, "choices.0.message.tool_calls.0.function.name").String(); got != "get_weather" {
		t.Fatalf("expected tool name get_weather, got %s", got)
	}
	if got := gjson.GetBytes(out, "choices.0.message.tool_calls.0.function.arguments").String(); got != `{"city":"LA"}` {
		t.Fatalf("expected args, got %s", got)
	}
}

func TestReverseResponseTranslatorRegistered(t *testing.T) {
	from := string(types.FormatGrokCLI)
	to := string(types.FormatOpenAI)

	if !registry.NeedConvert(from, to) {
		t.Fatalf("expected reverse response translator to be registered")
	}

	resp := []byte(`{"id":"resp_1","model":"grok-4.3","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Translated"}]}],"usage":{"input_tokens":5,"output_tokens":2}}`)
	out := registry.ResponseNonStream(context.Background(), from, to, "grok-cli/grok-4.3", nil, nil, resp, nil)

	if got := gjson.GetBytes(out, "choices.0.message.content").String(); got != "Translated" {
		t.Fatalf("expected content Translated, got %s", got)
	}
	if got := gjson.GetBytes(out, "choices.0.finish_reason").String(); got != "stop" {
		t.Fatalf("expected stop, got %s", got)
	}
	if got := gjson.GetBytes(out, "usage.prompt_tokens").Int(); got != 5 {
		t.Fatalf("expected prompt_tokens 5, got %d", got)
	}
	if got := gjson.GetBytes(out, "usage.completion_tokens").Int(); got != 2 {
		t.Fatalf("expected completion_tokens 2, got %d", got)
	}
}

func stringify(chunks [][]byte) string {
	var sb strings.Builder
	for i, c := range chunks {
		sb.WriteString(string(c))
		if i < len(chunks)-1 {
			sb.WriteString(" | ")
		}
	}
	return sb.String()
}
