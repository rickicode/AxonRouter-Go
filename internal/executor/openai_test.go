package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
	"github.com/tidwall/gjson"
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
			got, err := openAIEndpoint(tt.baseURL, tt.endpoint, nil)
			if err != nil {
				t.Fatalf("openAIEndpoint(%q, %q) unexpected error: %v", tt.baseURL, tt.endpoint, err)
			}
			if got != tt.want {
				t.Fatalf("openAIEndpoint(%q, %q) = %q, want %q", tt.baseURL, tt.endpoint, got, tt.want)
			}
		})
	}
}

func TestOpenAIEndpoint_AccountIdTemplate(t *testing.T) {
	base := "https://api.cloudflare.com/client/v4/accounts/{accountId}/ai/v1/chat/completions"

	t.Run("resolves from PSD", func(t *testing.T) {
		got, err := openAIEndpoint(base, "chat/completions", map[string]string{"accountId": "abc123"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "https://api.cloudflare.com/client/v4/accounts/abc123/ai/v1/chat/completions"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("resolves from env var when PSD empty", func(t *testing.T) {
		os.Setenv("CLOUDFLARE_ACCOUNT_ID", "env-account")
		defer os.Unsetenv("CLOUDFLARE_ACCOUNT_ID")
		got, err := openAIEndpoint(base, "chat/completions", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "https://api.cloudflare.com/client/v4/accounts/env-account/ai/v1/chat/completions"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("errors when no accountId available", func(t *testing.T) {
		os.Unsetenv("CLOUDFLARE_ACCOUNT_ID")
		_, err := openAIEndpoint(base, "chat/completions", nil)
		if err == nil {
			t.Fatal("expected error for missing accountId, got nil")
		}
	})
}

func TestOpenAIEndpoint_FullyQualifiedURLCollision(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		endpoint string
		want     string
	}{
		{
			name:     "CF chat base + embeddings strips chat suffix",
			baseURL:  "https://api.cloudflare.com/client/v4/accounts/abc/ai/v1/chat/completions",
			endpoint: "embeddings",
			want:     "https://api.cloudflare.com/client/v4/accounts/abc/ai/v1/embeddings",
		},
		{
			name:     "CF chat base + chat/completions stays unchanged",
			baseURL:  "https://api.cloudflare.com/client/v4/accounts/abc/ai/v1/chat/completions",
			endpoint: "chat/completions",
			want:     "https://api.cloudflare.com/client/v4/accounts/abc/ai/v1/chat/completions",
		},
		{
			name:     "normal base appends chat/completions",
			baseURL:  "https://api.openai.com/v1",
			endpoint: "chat/completions",
			want:     "https://api.openai.com/v1/chat/completions",
		},
		{
			name:     "CF chat base + models strips chat suffix",
			baseURL:  "https://api.cloudflare.com/client/v4/accounts/abc/ai/v1/chat/completions",
			endpoint: "models",
			want:     "https://api.cloudflare.com/client/v4/accounts/abc/ai/v1/models",
		},
		{
			name:     "CF responses base + chat/completions strips responses suffix",
			baseURL:  "https://api.cloudflare.com/client/v4/accounts/abc/ai/v1/responses",
			endpoint: "chat/completions",
			want:     "https://api.cloudflare.com/client/v4/accounts/abc/ai/v1/chat/completions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := openAIEndpoint(tt.baseURL, tt.endpoint, nil)
			if err != nil {
				t.Fatalf("openAIEndpoint(%q, %q) unexpected error: %v", tt.baseURL, tt.endpoint, err)
			}
			if got != tt.want {
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
		{"negative max_tokens", "@cf/meta/llama-3.2-1b-instruct", -1, 8192},
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

func TestSanitizeCFRequest_NormalizesModelPrefix(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  string
	}{
		{"already normalized @cf", "@cf/meta/llama-3.2-1b-instruct", "@cf/meta/llama-3.2-1b-instruct"},
		{"gateway full ID cf/", "cf/meta/llama-3.2-1b-instruct", "@cf/meta/llama-3.2-1b-instruct"},
		{"model name only", "meta/llama-3.2-1b-instruct", "@cf/meta/llama-3.2-1b-instruct"},
		{"moonshot gateway ID", "cf/moonshotai/kimi-k2.6", "@cf/moonshotai/kimi-k2.6"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"model": tt.model})
			out := sanitizeCFRequest(body)
			var got map[string]any
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got["model"] != tt.want {
				t.Errorf("model = %q, want %q", got["model"], tt.want)
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

	// First message: text flattened to string, thinking removed, image_url dropped.
	first := msgs[0].(map[string]any)
	contentStr, ok := first["content"].(string)
	if !ok || contentStr != "hello" {
		t.Errorf("expected content string 'hello', got %v", first["content"])
	}

	// Second message: tool_result converted to role:tool.
	second := msgs[1].(map[string]any)
	if second["role"] != "tool" {
		t.Errorf("expected role tool, got %v", second["role"])
	}
}

func TestSanitizeRequest_ConvertsDeveloperRole(t *testing.T) {
	tests := []struct {
		name string
		role string
		want string
	}{
		{"developer converts to system", "developer", "system"},
		{"system stays system", "system", "system"},
		{"user stays user", "user", "user"},
		{"assistant stays assistant", "assistant", "assistant"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := mustJSON(map[string]any{
				"model": "deepseek-chat",
				"messages": []any{
					map[string]any{"role": tt.role, "content": "You are helpful."},
					map[string]any{"role": "user", "content": "Hello"},
				},
			})
			out := sanitizeRequestWithCompatibility(body, providercfg.Compatibility{})

			var got map[string]any
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			msgs := got["messages"].([]any)
			first := msgs[0].(map[string]any)
			if first["role"] != tt.want {
				t.Errorf("role = %v, want %v", first["role"], tt.want)
			}
		})
	}
}

func TestSanitizeRequest_ConvertsMultipleDeveloperMessages(t *testing.T) {
	body := mustJSON(map[string]any{
		"model": "deepseek-chat",
		"messages": []any{
			map[string]any{"role": "developer", "content": "System instruction 1"},
			map[string]any{"role": "developer", "content": "System instruction 2"},
			map[string]any{"role": "user", "content": "Hello"},
		},
	})
	out := sanitizeRequestWithCompatibility(body, providercfg.Compatibility{})

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	msgs := got["messages"].([]any)
	for i, raw := range msgs {
		msg := raw.(map[string]any)
		if i < 2 && msg["role"] != "system" {
			t.Errorf("message[%d].role = %v, want system", i, msg["role"])
		}
		if i == 2 && msg["role"] != "user" {
			t.Errorf("message[%d].role = %v, want user", i, msg["role"])
		}
	}
}

func TestSplitModel_StripsAtPrefix(t *testing.T) {
	tests := []struct {
		model      string
		wantPrefix string
		wantName   string
	}{
		{"openai/gpt-4o", "openai", "gpt-4o"},
		{"@cf/moonshotai/kimi-k2.6", "cf", "moonshotai/kimi-k2.6"},
		{"@cf/meta/llama-3.2-1b-instruct", "cf", "meta/llama-3.2-1b-instruct"},
		{"deepseek/deepseek-chat", "deepseek", "deepseek-chat"},
		{"gpt-4o", "", "gpt-4o"},
	}
	for _, tt := range tests {
		prefix, name := SplitModel(tt.model)
		if prefix != tt.wantPrefix {
			t.Errorf("SplitModel(%q) prefix = %q, want %q", tt.model, prefix, tt.wantPrefix)
		}
		if name != tt.wantName {
			t.Errorf("SplitModel(%q) name = %q, want %q", tt.model, name, tt.wantName)
		}
	}
}

func TestOpenAIExecutor_Images(t *testing.T) {
	var calledPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"created":1,"data":[]}`)
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	openai := NewOpenAIExecutor(base)
	req := &Request{
		Model:   "dall-e-3",
		BaseURL: ts.URL + "/v1/chat/completions",
		Body:    mustJSON(map[string]any{"model": "dall-e-3", "prompt": "a cat"}),
	}
	resp, err := openai.Images(context.Background(), req)
	if err != nil {
		t.Fatalf("Images error: %v", err)
	}
	if calledPath != "/v1/images/generations" {
		t.Fatalf("expected path /v1/images/generations, got %s", calledPath)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
}

func TestNormalizeDeveloperRoles(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantRole string
	}{
		{
			name:     "converts developer to system",
			body:     `{"model":"deepseek-chat","messages":[{"role":"developer","content":"instructions"},{"role":"user","content":"hi"}]}`,
			wantRole: "system",
		},
		{
			name:     "no-op when no developer role",
			body:     `{"model":"deepseek-chat","messages":[{"role":"system","content":"instructions"},{"role":"user","content":"hi"}]}`,
			wantRole: "system",
		},
		{
			name:     "converts multiple developer messages",
			body:     `{"model":"deepseek-chat","messages":[{"role":"developer","content":"a"},{"role":"developer","content":"b"},{"role":"user","content":"hi"}]}`,
			wantRole: "system",
		},
		{
			name:     "no-op when no messages field",
			body:     `{"model":"deepseek-chat"}`,
			wantRole: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := normalizeDeveloperRoles([]byte(tt.body))
			msgs := gjson.GetBytes(out, "messages").Array()
			if tt.wantRole == "" {
				if len(msgs) != 0 {
					t.Errorf("expected no messages, got %d", len(msgs))
				}
				return
			}
			for i, msg := range msgs {
				role := msg.Get("role").String()
				if role == "developer" {
					t.Errorf("message[%d].role still developer after conversion", i)
				}
			}
			if msgs[0].Get("role").String() != tt.wantRole {
				t.Errorf("first message role = %s, want %s", msgs[0].Get("role").String(), tt.wantRole)
			}
		})
	}
}

func TestSanitizeDeepSeekThinkingMode(t *testing.T) {
	tests := []struct {
		name             string
		body             string
		wantReasoningEffort bool
	}{
		{
			name:             "strips reasoning_effort when assistant missing reasoning_content",
			body:             `{"model":"deepseek-r1","reasoning_effort":"high","messages":[{"role":"system","content":"hi"},{"role":"user","content":"hello"},{"role":"assistant","content":"hey"}]}`,
			wantReasoningEffort: false,
		},
		{
			name:             "keeps reasoning_effort when assistant has reasoning_content",
			body:             `{"model":"deepseek-r1","reasoning_effort":"high","messages":[{"role":"system","content":"hi"},{"role":"user","content":"hello"},{"role":"assistant","content":"hey","reasoning_content":"thinking..."}]}`,
			wantReasoningEffort: true,
		},
		{
			name:             "no-op when no reasoning_effort",
			body:             `{"model":"deepseek-r1","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hey"}]}`,
			wantReasoningEffort: false,
		},
		{
			name:             "no-op when reasoning_effort is none",
			body:             `{"model":"deepseek-r1","reasoning_effort":"none","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hey"}]}`,
			wantReasoningEffort: true,
		},
		{
			name:             "no-op when no assistant messages",
			body:             `{"model":"deepseek-r1","reasoning_effort":"high","messages":[{"role":"user","content":"hello"}]}`,
			wantReasoningEffort: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := sanitizeDeepSeekThinkingMode([]byte(tt.body))
			hasRE := gjson.GetBytes(out, "reasoning_effort").Exists()
			if hasRE != tt.wantReasoningEffort {
				t.Errorf("reasoning_effort exists = %v, want %v", hasRE, tt.wantReasoningEffort)
			}
		})
	}
}
