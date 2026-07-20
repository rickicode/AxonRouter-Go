package executor

import (
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

func TestSanitizeCodeBuddyChunkStripsReasoning(t *testing.T) {
	input := `data: {"id":"cmb-1","object":"chat.completion.chunk","created":1,"model":"glm-5.2","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi","reasoning_content":"hidden thought","function_call":null},"finish_reason":""}]}`
	got := string(sanitizeCodeBuddyChunk([]byte(input)))
	if strings.Contains(got, "reasoning_content") {
		t.Errorf("sanitized chunk still contains reasoning_content: %s", got)
	}
	if !strings.Contains(got, `"content":"Hi"`) {
		t.Errorf("sanitized chunk lost content: %s", got)
	}
}

func TestSanitizeCodeBuddyChunkStripsEmptyReasoning(t *testing.T) {
	input := `data: {"id":"cmb-1","object":"chat.completion.chunk","created":1,"model":"glm-5.2","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi","reasoning_content":"","function_call":null},"finish_reason":""}]}`
	got := string(sanitizeCodeBuddyChunk([]byte(input)))
	if strings.Contains(got, "reasoning_content") {
		t.Errorf("sanitized chunk still contains empty reasoning_content: %s", got)
	}
}

func TestSanitizeCodeBuddyChunkStripsNullReasoning(t *testing.T) {
	input := `data: {"id":"cmb-1","object":"chat.completion.chunk","created":1,"model":"glm-5.2","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi","reasoning_content":null,"function_call":null},"finish_reason":""}]}`
	got := string(sanitizeCodeBuddyChunk([]byte(input)))
	if strings.Contains(got, "reasoning_content") {
		t.Errorf("sanitized chunk still contains null reasoning_content: %s", got)
	}
}

func TestSanitizeCodeBuddyChunkStripsIntermediateUsage(t *testing.T) {
	input := `data: {"id":"cmb-1","object":"chat.completion.chunk","created":1,"model":"glm-5.2","choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":""}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`
	got := string(sanitizeCodeBuddyChunk([]byte(input)))
	if strings.Contains(got, "usage") {
		t.Errorf("sanitized intermediate chunk still contains usage: %s", got)
	}
}

func TestSanitizeCodeBuddyChunkKeepsFinalUsage(t *testing.T) {
	input := `data: {"id":"cmb-1","object":"chat.completion.chunk","created":1,"model":"glm-5.2","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`
	got := string(sanitizeCodeBuddyChunk([]byte(input)))
	if !strings.Contains(got, "usage") {
		t.Errorf("sanitized final chunk lost usage: %s", got)
	}
}

func TestSanitizeCodeBuddyChunkCleansJunkFields(t *testing.T) {
	input := `data: {"id":"cmb-1","object":"chat.completion.chunk","created":1,"model":"glm-5.2","choices":[{"index":0,"delta":{"content":"Hi","extra_fields":null,"function_call":null,"refusal":"","role":"assistant","tool_calls":[]},"finish_reason":"","logprobs":null}],"usage":null}`
	got := string(sanitizeCodeBuddyChunk([]byte(input)))
	for _, field := range []string{"extra_fields", "function_call", "refusal", "tool_calls", "logprobs", "usage"} {
		if strings.Contains(got, field) {
			t.Errorf("sanitized chunk still contains junk field %q: %s", field, got)
		}
	}
	if !strings.Contains(got, `"content":"Hi"`) {
		t.Errorf("sanitized chunk lost content: %s", got)
	}
}

func TestSanitizeCodeBuddyChunkPassesDone(t *testing.T) {
	input := []byte("data: [DONE]")
	got := string(sanitizeCodeBuddyChunk(input))
	if got != string(input) {
		t.Errorf("sanitized [DONE] changed: %s", got)
	}
}
