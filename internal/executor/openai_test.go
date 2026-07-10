package executor

import (
	"encoding/json"
	"testing"
)

func TestOpenAIEndpointNormalizesBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		endpoint string
		want     string
	}{
		{
			name:     "empty base uses OpenAI default",
			endpoint: "chat/completions",
			want:     "https://api.openai.com/v1/chat/completions",
		},
		{
			name:     "root OpenAI compatible base appends chat completions",
			baseURL:  "https://opencode.ai/zen/v1",
			endpoint: "chat/completions",
			want:     "https://opencode.ai/zen/v1/chat/completions",
		},
		{
			name:     "trailing slash root base appends models",
			baseURL:  "https://opencode.ai/zen/v1/",
			endpoint: "models",
			want:     "https://opencode.ai/zen/v1/models",
		},
		{
			name:     "full chat completions endpoint stays unchanged",
			baseURL:  "https://example.com/v1/chat/completions",
			endpoint: "chat/completions",
			want:     "https://example.com/v1/chat/completions",
		},
		{
			name:     "full responses endpoint stays unchanged",
			baseURL:  "https://example.com/v1/responses",
			endpoint: "responses",
			want:     "https://example.com/v1/responses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := openAIEndpoint(tt.baseURL, tt.endpoint, nil); got != tt.want {
				t.Fatalf("openAIEndpoint(%q, %q) = %q, want %q", tt.baseURL, tt.endpoint, got, tt.want)
			}
		})
	}
}

func TestSanitizeCFRequest_CapsMaxTokens(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		current any
		want    float64
	}{
		{"reasoning deepseek", "@cf/deepseek-ai/deepseek-r1-distill-qwen-32b", nil, 4096},
		{"reasoning qwq", "@cf/qwen/qwq-32b", nil, 4096},
		{"reasoning kimi k2.5", "@cf/moonshotai/kimi-k2.5", nil, 4096},
		{"reasoning glm-5.2", "@cf/zai-org/glm-5.2", 16384, 4096},
		{"normal llama", "@cf/meta/llama-3.2-1b-instruct", 20000, 8192},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := map[string]any{"model": tt.model}
			if tt.current != nil {
				req["max_tokens"] = tt.current
			}
			body, _ := json.Marshal(req)
			out := sanitizeCFRequest(body)

			var got map[string]any
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got["max_tokens"] != tt.want {
				t.Errorf("max_tokens = %v, want %v", got["max_tokens"], tt.want)
			}
		})
	}
}

func TestSanitizeCFRequest_FiltersContentBlocks(t *testing.T) {
	req := map[string]any{
		"model": "@cf/meta/llama-3.2-1b-instruct",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "hello"},
					map[string]any{"type": "thinking", "thinking": "..."},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/x.png"}},
				},
			},
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "tool_result", "tool_use_id": "t1", "content": "result"},
				},
			},
		},
	}
	body, _ := json.Marshal(req)
	out := sanitizeCFRequest(body)

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	msgs, ok := got["messages"].([]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	// First message: text + image_url kept, thinking removed.
	first := msgs[0].(map[string]any)
	content, ok := first["content"].([]any)
	if !ok || len(content) != 2 {
		t.Errorf("expected 2 safe blocks, got %d", len(content))
	}

	// Second message: tool_result converted to role:tool.
	second := msgs[1].(map[string]any)
	if second["role"] != "tool" {
		t.Errorf("expected role tool, got %v", second["role"])
	}
}
