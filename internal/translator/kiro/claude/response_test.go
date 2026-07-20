package claude

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func dataLine(line string) []byte {
	return []byte("data: " + line)
}

func collectEvents(t *testing.T, chunks [][]byte) string {
	t.Helper()
	var b strings.Builder
	for _, c := range chunks {
		b.Write(bytes.TrimSpace(c))
		b.WriteByte('\n')
	}
	return b.String()
}

func TestKiroToClaudeResponse_Reasoning(t *testing.T) {
	ctx := context.Background()
	var param any

	chunk1 := dataLine(`{"id":"chatcmpl-reason","object":"chat.completion.chunk","model":"test-model","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"Thinking step"}}]}`)
	out1 := ConvertKiroResponseToClaudeStream(ctx, "test-model", nil, nil, chunk1, &param)
	if len(out1) == 0 {
		t.Fatalf("expected events on first chunk, got none")
	}

	done := dataLine("[DONE]")
	out2 := ConvertKiroResponseToClaudeStream(ctx, "test-model", nil, nil, done, &param)
	all := collectEvents(t, append(out1, out2...))

	if !strings.Contains(all, `"type":"thinking_delta"`) {
		t.Fatalf("expected thinking_delta event in output:\n%s", all)
	}
	if !strings.Contains(all, `"thinking":"Thinking step"`) {
		t.Fatalf("expected thinking content in output:\n%s", all)
	}
	if !strings.Contains(all, `"type":"message_start"`) {
		t.Fatalf("expected message_start event in output:\n%s", all)
	}
}

func TestKiroToClaudeResponse_ToolCall(t *testing.T) {
	ctx := context.Background()
	var param any

	chunk1 := dataLine(`{"id":"chatcmpl-tool","object":"chat.completion.chunk","model":"test-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_abc","function":{"name":"weather","arguments":"{\"city\":\""}}]}}]}`)
	chunk2 := dataLine(`{"id":"chatcmpl-tool","object":"chat.completion.chunk","model":"test-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"New York\"}"}}]},"finish_reason":"tool_calls"}]}`)

	out1 := ConvertKiroResponseToClaudeStream(ctx, "test-model", nil, nil, chunk1, &param)
	out2 := ConvertKiroResponseToClaudeStream(ctx, "test-model", nil, nil, chunk2, &param)
	done := dataLine("[DONE]")
	out3 := ConvertKiroResponseToClaudeStream(ctx, "test-model", nil, nil, done, &param)

	all := collectEvents(t, append(append(out1, out2...), out3...))

	if !strings.Contains(all, `"type":"tool_use"`) {
		t.Fatalf("expected tool_use content_block_start in output:\n%s", all)
	}
	if !strings.Contains(all, `"type":"input_json_delta"`) {
		t.Fatalf("expected input_json_delta event in output:\n%s", all)
	}
	if !strings.Contains(all, `"partial_json":"{\"city\":\"New York\"}"`) {
		t.Fatalf("expected accumulated tool arguments in output:\n%s", all)
	}
}
