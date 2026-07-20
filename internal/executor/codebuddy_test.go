package executor

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCodeBuddyHeaders(t *testing.T) {
	tests := []struct {
		provider    string
		wantHeaders map[string]string
	}{
		{
			provider: "codebuddy",
			wantHeaders: map[string]string{
				"User-Agent":          "CLI/2.63.2 CodeBuddy/2.63.2",
				"X-Product":           "SaaS",
				"X-IDE-Type":          "CLI",
				"X-IDE-Name":          "CLI",
				"X-Domain":            "www.codebuddy.ai",
				"x-requested-with":    "XMLHttpRequest",
				"x-codebuddy-request": "1",
			},
		},
		{
			provider:    "openai",
			wantHeaders: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			headers := map[string]string{}
			codebuddyHeaders(headers, tt.provider)
			if len(headers) != len(tt.wantHeaders) {
				t.Fatalf("got %d headers, want %d", len(headers), len(tt.wantHeaders))
			}
			for k, v := range tt.wantHeaders {
				if got := headers[k]; got != v {
					t.Errorf("headers[%q] = %q, want %q", k, got, v)
				}
			}
		})
	}
}

func TestCodeBuddyInjectThinking(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		body      string
		wantAdded bool
	}{
		{"reasoning model without config", "glm-5.2", `{"messages":[]}`, true},
		{"reasoning model with reasoning_effort", "glm-5.2", `{"reasoning_effort":"medium","messages":[]}`, false},
		{"reasoning model with thinking", "deepseek-v4-pro", `{"thinking":{},"messages":[]}`, false},
		{"non-reasoning model", "codebuddy-minimax-m3", `{"messages":[]}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(codebuddyMaybeInjectThinking([]byte(tt.body), tt.model))
			hasReasoning := strings.Contains(got, `"reasoning_effort":"high"`)
			if hasReasoning != tt.wantAdded {
				t.Errorf("body=%s, want added=%v, got added=%v", got, tt.wantAdded, hasReasoning)
			}
		})
	}
}

func TestNormalizeCodeBuddyStreamAggregatesReasoning(t *testing.T) {
	in := make(chan StreamChunk, 5)
	in <- StreamChunk{Payload: []byte(`data: {"id":"cmb-1","object":"chat.completion.chunk","created":1,"model":"glm-5.2","choices":[{"index":0,"delta":{"role":"assistant","content":"","reasoning_content":"Think "},"finish_reason":""}]}`)}
	in <- StreamChunk{Payload: []byte(`data: {"id":"cmb-1","object":"chat.completion.chunk","created":1,"model":"glm-5.2","choices":[{"index":0,"delta":{"reasoning_content":"step "},"finish_reason":""}]}`)}
	in <- StreamChunk{Payload: []byte(`data: {"id":"cmb-1","object":"chat.completion.chunk","created":1,"model":"glm-5.2","choices":[{"index":0,"delta":{"reasoning_content":"by step"},"finish_reason":""}]}`)}
	in <- StreamChunk{Payload: []byte(`data: {"id":"cmb-1","object":"chat.completion.chunk","created":1,"model":"glm-5.2","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":""}]}`)}
	in <- StreamChunk{Payload: []byte("data: [DONE]")}
	close(in)

	var chunks []string
	for chunk := range normalizeCodeBuddyStream(in) {
		chunks = append(chunks, string(chunk.Payload))
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 output chunks, got %d:\n%s", len(chunks), strings.Join(chunks, "\n"))
	}
	if !strings.Contains(chunks[0], `"reasoning_content":"Think step by step"`) {
		t.Errorf("first chunk should contain aggregated reasoning: %s", chunks[0])
	}
	if !strings.Contains(chunks[1], `"content":"Hello"`) {
		t.Errorf("second chunk should contain content: %s", chunks[1])
	}
	if chunks[2] != "data: [DONE]" {
		t.Errorf("third chunk should be DONE: %s", chunks[2])
	}
}

func TestNormalizeCodeBuddyStreamNoReasoningPassesContent(t *testing.T) {
	in := make(chan StreamChunk, 3)
	in <- StreamChunk{Payload: []byte(`data: {"id":"cmb-2","object":"chat.completion.chunk","created":1,"model":"glm-5.2","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"},"finish_reason":""}]}`)}
	in <- StreamChunk{Payload: []byte(`data: {"id":"cmb-2","object":"chat.completion.chunk","created":1,"model":"glm-5.2","choices":[{"index":0,"delta":{"content":" there"},"finish_reason":""}]}`)}
	in <- StreamChunk{Payload: []byte("data: [DONE]")}
	close(in)

	var chunks []string
	for chunk := range normalizeCodeBuddyStream(in) {
		chunks = append(chunks, string(chunk.Payload))
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 output chunks, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0], `"content":"Hi"`) {
		t.Errorf("first chunk lost content: %s", chunks[0])
	}
	if strings.Contains(chunks[0], "reasoning_content") {
		t.Errorf("first chunk should not have reasoning_content: %s", chunks[0])
	}
}

func TestNormalizeCodeBuddyStreamStripsIntermediateUsage(t *testing.T) {
	in := make(chan StreamChunk, 3)
	in <- StreamChunk{Payload: []byte(`data: {"id":"cmb-3","object":"chat.completion.chunk","created":1,"model":"glm-5.2","choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":""}],"usage":{"prompt_tokens":1}}`)} // intermediate usage stripped
	in <- StreamChunk{Payload: []byte(`data: {"id":"cmb-3","object":"chat.completion.chunk","created":1,"model":"glm-5.2","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)} // final usage kept
	in <- StreamChunk{Payload: []byte("data: [DONE]")}
	close(in)

	var chunks []string
	for chunk := range normalizeCodeBuddyStream(in) {
		chunks = append(chunks, string(chunk.Payload))
	}

	if strings.Contains(chunks[0], "usage") {
		t.Errorf("intermediate chunk still contains usage: %s", chunks[0])
	}
	if !strings.Contains(chunks[1], `"usage":`) {
		t.Errorf("final chunk should keep usage: %s", chunks[1])
	}
}

func TestNormalizeCodeBuddyStreamCleansNoise(t *testing.T) {
	in := make(chan StreamChunk, 2)
	in <- StreamChunk{Payload: []byte(`data: {"id":"cmb-4","object":"chat.completion.chunk","created":1,"model":"glm-5.2","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi","extra_fields":null,"function_call":null,"refusal":"","tool_calls":[]},"finish_reason":"","logprobs":null}],"usage":null}`)}
	in <- StreamChunk{Payload: []byte("data: [DONE]")}
	close(in)

	var chunks []string
	for chunk := range normalizeCodeBuddyStream(in) {
		chunks = append(chunks, string(chunk.Payload))
	}

	got := chunks[0]
	for _, field := range []string{"extra_fields", "function_call", "refusal", "tool_calls", "logprobs", "usage"} {
		if strings.Contains(got, field) {
			t.Errorf("output still contains noise field %q: %s", field, got)
		}
	}
	if !strings.Contains(got, `"content":"Hi"`) {
		t.Errorf("output lost content: %s", got)
	}
}

func TestAggregateCodeBuddyStreamPreservesReasoning(t *testing.T) {
	ch := make(chan StreamChunk, 4)
	ch <- StreamChunk{Payload: []byte(`data: {"id":"cmb-5","object":"chat.completion.chunk","created":1,"model":"glm-5.2","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"Think step"},"finish_reason":""}]}`)}
	ch <- StreamChunk{Payload: []byte(`data: {"id":"cmb-5","object":"chat.completion.chunk","created":1,"model":"glm-5.2","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":""}]}`)}
	ch <- StreamChunk{Payload: []byte(`data: {"id":"cmb-5","object":"chat.completion.chunk","created":1,"model":"glm-5.2","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":""}]}`)}
	ch <- StreamChunk{Payload: []byte("data: [DONE]")}
	close(ch)

	got, err := aggregateCodeBuddyStream(ch)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if !bytes.Contains(got, []byte(`"reasoning_content":"Think step"`)) {
		t.Errorf("expected reasoning_content in response: %s", string(got))
	}
	if !bytes.Contains(got, []byte(`"content":"Hello world"`)) {
		t.Errorf("expected merged content in response: %s", string(got))
	}

	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	choices := parsed["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	if rc := message["reasoning_content"]; rc != "Think step" {
		t.Errorf("reasoning_content = %q, want %q", rc, "Think step")
	}
}
