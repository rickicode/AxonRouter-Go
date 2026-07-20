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

func TestSanitizeCodeBuddyChunkPassesDone(t *testing.T) {
	input := []byte("data: [DONE]")
	got := string(sanitizeCodeBuddyChunk(input))
	if got != string(input) {
		t.Errorf("sanitized [DONE] changed: %s", got)
	}
}
