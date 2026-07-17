package responses

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func chunk(s string) []byte {
	return []byte("data: " + s + "\n\n")
}

func collectStream(t *testing.T, originalReq string, events ...string) [][]byte {
	t.Helper()
	var param any
	var out [][]byte
	for _, e := range events {
		chunks := convertCodexResponseToOpenAIStream(context.Background(), "cx/gpt-5.4", []byte(originalReq), nil, chunk(e), &param)
		out = append(out, chunks...)
	}
	return out
}

func TestStreamTextDelta(t *testing.T) {
	events := []string{
		`{"type":"response.created","response":{"id":"resp_1","created_at":1700000000,"model":"gpt-5.4"}}`,
		`{"type":"response.output_text.delta","delta":"Hello"}`,
		`{"type":"response.completed"}`,
	}
	chunks := collectStream(t, "{}", events...)
	if len(chunks) != 2 { // text chunk + completed chunk; [DONE] is emitted by handler on EOF
		t.Fatalf("expected 2 chunks, got %d: %s", len(chunks), stringify(chunks))
	}
	textChunk := chunks[0]
	if !bytes.HasPrefix(textChunk, []byte("data: ")) {
		t.Fatalf("expected SSE prefix, got %s", textChunk)
	}
	jsonPart := bytes.TrimSpace(textChunk[5:])
	if gjson.GetBytes(jsonPart, "choices.0.delta.content").String() != "Hello" {
		t.Fatalf("expected Hello, got %s", gjson.GetBytes(jsonPart, "choices.0.delta.content"))
	}
}

func TestStreamReasoningDelta(t *testing.T) {
	events := []string{
		`{"type":"response.created","response":{"id":"resp_1","created_at":1700000000,"model":"gpt-5.4"}}`,
		`{"type":"response.reasoning_summary_text.delta","delta":"thinking..."}`,
		`{"type":"response.completed"}`,
	}
	chunks := collectStream(t, "{}", events...)
	jsonPart := bytes.TrimSpace(chunks[0][5:])
	if got := gjson.GetBytes(jsonPart, "choices.0.delta.reasoning_content").String(); got != "thinking..." {
		t.Fatalf("expected reasoning content, got %s", got)
	}
}

func TestStreamFunctionCallArgumentStreaming(t *testing.T) {
	events := []string{
		`{"type":"response.created","response":{"id":"resp_1","created_at":1700000000}}`,
		`{"type":"response.output_item.added","item":{"type":"function_call","call_id":"call_1","name":"get_weather"}}`,
		`{"type":"response.function_call_arguments.delta","delta":"{\"city\": \"L"}`,
		`{"type":"response.function_call_arguments.delta","delta":"A\"}"}`,
		`{"type":"response.function_call_arguments.done"}`,
		`{"type":"response.completed"}`,
	}
	originalReq := `{"tools":[{"type":"function","function":{"name":"get_weather"}}]}`
	chunks := collectStream(t, originalReq, events...)
	// We expect: added chunk, arg delta 1, arg delta 2, completed (DONE is emitted by handler on EOF)
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks, got %d: %s", len(chunks), stringify(chunks))
	}
	added := bytes.TrimSpace(chunks[0][5:])
	if gjson.GetBytes(added, "choices.0.delta.tool_calls.0.function.name").String() != "get_weather" {
		t.Fatalf("expected tool call announcement, got %s", added)
	}
	arg1 := bytes.TrimSpace(chunks[1][5:])
	if got := gjson.GetBytes(arg1, "choices.0.delta.tool_calls.0.function.arguments").String(); got != `{"city": "L` {
		t.Fatalf("expected first arg delta, got %s", got)
	}
	arg2 := bytes.TrimSpace(chunks[2][5:])
	if got := gjson.GetBytes(arg2, "choices.0.delta.tool_calls.0.function.arguments").String(); got != `A"}` {
		t.Fatalf("expected second arg delta, got %s", got)
	}
}

func TestStreamFunctionCallWithoutDelta(t *testing.T) {
	events := []string{
		`{"type":"response.created","response":{"id":"resp_1","created_at":1700000000}}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"LA\"}"}}`,
		`{"type":"response.completed"}`,
	}
	originalReq := `{"tools":[{"type":"function","function":{"name":"get_weather"}}]}`
	chunks := collectStream(t, originalReq, events...)
	if len(chunks) != 2 { // function_call chunk + completed chunk
		t.Fatalf("expected 2 chunks, got %d: %s", len(chunks), stringify(chunks))
	}
	fc := bytes.TrimSpace(chunks[0][5:])
	if got := gjson.GetBytes(fc, "choices.0.delta.tool_calls.0.function.arguments").String(); got != `{"city":"LA"}` {
		t.Fatalf("expected args, got %s", got)
	}
	completed := bytes.TrimSpace(chunks[1][5:])
	if got := gjson.GetBytes(completed, "choices.0.finish_reason").String(); got != "tool_calls" {
		t.Fatalf("expected finish_reason tool_calls, got %s", got)
	}
}

func TestStreamImageGenerationDedup(t *testing.T) {
	events := []string{
		`{"type":"response.created","response":{"id":"resp_1","created_at":1700000000}}`,
		`{"type":"response.image_generation_call.partial_image","item_id":"img_1","partial_image_b64":"abc123","output_format":"png"}`,
		`{"type":"response.image_generation_call.partial_image","item_id":"img_1","partial_image_b64":"abc123","output_format":"png"}`,
		`{"type":"response.completed"}`,
	}
	chunks := collectStream(t, "{}", events...)
	// Only one image chunk + completed; [DONE] is emitted by handler on EOF
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks after dedup, got %d: %s", len(chunks), stringify(chunks))
	}
}

func TestStreamCompletedUsage(t *testing.T) {
	events := []string{
		`{"type":"response.created","response":{"id":"resp_1","created_at":1700000000,"model":"gpt-5.4"}}`,
		`{"type":"response.output_text.delta","delta":"Hi"}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":2}}}}`,
	}
	chunks := collectStream(t, "{}", events...)
	completed := bytes.TrimSpace(chunks[1][5:])
	if got := gjson.GetBytes(completed, "usage.prompt_tokens").Int(); got != 10 {
		t.Fatalf("expected prompt_tokens 10, got %d", got)
	}
	if got := gjson.GetBytes(completed, "usage.completion_tokens").Int(); got != 5 {
		t.Fatalf("expected completion_tokens 5, got %d", got)
	}
	if got := gjson.GetBytes(completed, "usage.total_tokens").Int(); got != 15 {
		t.Fatalf("expected total_tokens 15, got %d", got)
	}
	if got := gjson.GetBytes(completed, "usage.prompt_tokens_details.cached_tokens").Int(); got != 3 {
		t.Fatalf("expected cached_tokens 3, got %d", got)
	}
	if got := gjson.GetBytes(completed, "usage.completion_tokens_details.reasoning_tokens").Int(); got != 2 {
		t.Fatalf("expected reasoning_tokens 2, got %d", got)
	}
}

func TestNonStreamTextAndReasoning(t *testing.T) {
	resp := []byte(`{"response":{"id":"resp_2","created_at":1700000001,"model":"gpt-5.4","status":"completed","output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"step 1"}]},{"type":"message","content":[{"type":"output_text","text":"Done"}]}],"usage":{"input_tokens":7,"output_tokens":3,"total_tokens":10}}}`)
	out := convertCodexResponseToOpenAINonStream(context.Background(), "cx/gpt-5.4", []byte(`{}`), nil, resp, nil)
	if got := gjson.GetBytes(out, "choices.0.message.content").String(); got != "Done" {
		t.Fatalf("expected content Done, got %s", got)
	}
	if got := gjson.GetBytes(out, "choices.0.message.reasoning_content").String(); got != "step 1" {
		t.Fatalf("expected reasoning, got %s", got)
	}
	if got := gjson.GetBytes(out, "choices.0.finish_reason").String(); got != "stop" {
		t.Fatalf("expected stop, got %s", got)
	}
	if got := gjson.GetBytes(out, "usage.prompt_tokens").Int(); got != 7 {
		t.Fatalf("expected prompt_tokens 7, got %d", got)
	}
}

func TestNonStreamFunctionCall(t *testing.T) {
	resp := []byte(`{"response":{"id":"resp_3","created_at":1700000002,"model":"gpt-5.4","status":"completed","output":[{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"LA\"}"}]}}`)
	originalReq := `{"tools":[{"type":"function","function":{"name":"get_weather"}}]}`
	out := convertCodexResponseToOpenAINonStream(context.Background(), "cx/gpt-5.4", []byte(originalReq), nil, resp, nil)
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

func TestNonStreamImageGeneration(t *testing.T) {
	resp := []byte(`{"response":{"id":"resp_4","created_at":1700000003,"model":"gpt-5.4","status":"completed","output":[{"type":"image_generation_call","result":"b64data","output_format":"png"}]}}`)
	out := convertCodexResponseToOpenAINonStream(context.Background(), "cx/gpt-5.4", []byte(`{}`), nil, resp, nil)
	url := gjson.GetBytes(out, "choices.0.message.images.0.image_url.url").String()
	if !strings.HasPrefix(url, "data:image/png;base64,b64data") {
		t.Fatalf("expected png data url, got %s", url)
	}
}

func TestToolNameRestoredFromShortName(t *testing.T) {
	longName := strings.Repeat("a", 70)
	events := []string{
		`{"type":"response.created","response":{"id":"resp_1","created_at":1700000000}}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","call_id":"call_1","name":"` + buildShortNameMap([]string{longName})[longName] + `","arguments":"{}"}}`,
		`{"type":"response.completed"}`,
	}
	originalReq := `{"tools":[{"type":"function","function":{"name":"` + longName + `"}}]}`
	chunks := collectStream(t, originalReq, events...)
	fc := bytes.TrimSpace(chunks[0][5:])
	if got := gjson.GetBytes(fc, "choices.0.delta.tool_calls.0.function.name").String(); got != longName {
		t.Fatalf("expected original long name restored, got %s", got)
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
