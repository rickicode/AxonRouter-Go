package executor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
	"github.com/tidwall/gjson"
)

func TestZenMuxUpstreamModelNotDoublePrefixed(t *testing.T) {
	var gotModel string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotModel = gjson.GetBytes(body, "model").String()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 1700000000,
			"model":   gotModel,
			"choices": []map[string]any{
				{
					"index":        0,
					"message":      map[string]any{"role": "assistant", "content": "hi"},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer ts.Close()

	exec := NewOpenAIExecutor(NewBaseExecutor())
	body := []byte(`{"model":"openai/gpt-5.6-luna","messages":[{"role":"user","content":"hello"}]}`)
	_, err := exec.Execute(context.Background(), &Request{
		Model:       "openai/gpt-5.6-luna",
		Body:        body,
		APIKey:      "sk-test",
		BaseURL:     ts.URL,
		Provider:    "zenmux",
		StreamConfig: &StreamConfig{},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if gotModel != "openai/gpt-5.6-luna" {
		t.Fatalf("upstream model = %q, want openai/gpt-5.6-luna", gotModel)
	}
}

func TestZenMuxCompatibilityStripsGatewayPrefix(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  string
	}{
		{"strips gateway prefix", "zenmux/openai/gpt-5.6-luna", "openai/gpt-5.6-luna"},
		{"strips free gateway prefix", "zenmux-free/z-ai/glm-5.2", "z-ai/glm-5.2"},
		{"leaves upstream id unchanged", "openai/gpt-5.6-luna", "openai/gpt-5.6-luna"},
		{"leaves free upstream id unchanged", "z-ai/glm-5.2", "z-ai/glm-5.2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := "zenmux"
			if strings.HasPrefix(tt.model, "zenmux-free/") {
				provider = "zenmux-free"
			}
			c := providercfg.CompatibilityFor(provider)
			body, _ := json.Marshal(map[string]any{"model": tt.model})
			out := sanitizeRequestWithCompatibility(body, c)
			got := gjson.GetBytes(out, "model").String()
			if got != tt.want {
				t.Fatalf("sanitizeRequestWithCompatibility(%q) model = %q, want %q", tt.model, got, tt.want)
			}
		})
	}
}
