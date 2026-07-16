package executor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
	"github.com/tidwall/gjson"
)

func TestBedrockResolveBaseURL(t *testing.T) {
	tests := []struct {
		name   string
		base   string
		psd    map[string]string
		want   string
	}{
		{
			name: "default region",
			base: "",
			psd:  nil,
			want: "https://bedrock-mantle.us-west-2.api.aws/v1",
		},
		{
			name: "region from psd",
			base: "",
			psd:  map[string]string{"region": "eu-west-1"},
			want: "https://bedrock-mantle.eu-west-1.api.aws/v1",
		},
		{
			name: "custom base preserved",
			base: "https://bedrock-mantle.ap-south-1.api.aws/v1",
			psd:  map[string]string{"region": "us-east-1"},
			want: "https://bedrock-mantle.ap-south-1.api.aws/v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bedrockBaseURL(tt.base, tt.psd)
			if got != tt.want {
				t.Fatalf("bedrockBaseURL(%q, %v) = %q, want %q", tt.base, tt.psd, got, tt.want)
			}
		})
	}
}

func TestBedrockStripModelPrefix(t *testing.T) {
	tests := []struct {
		name string
		model string
		want string
	}{
		{"with prefix", "bedrock/us.anthropic.claude-3-7-sonnet-20250219-v1:0", "us.anthropic.claude-3-7-sonnet-20250219-v1:0"},
		{"without prefix", "us.anthropic.claude-3-7-sonnet-20250219-v1:0", "us.anthropic.claude-3-7-sonnet-20250219-v1:0"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"model": tt.model})
			c := providercfg.CompatibilityFor("bedrock")
			out := sanitizeRequestWithCompatibility(body, c)
			got := gjson.GetBytes(out, "model").String()
			if got != tt.want {
				t.Fatalf("sanitizeRequestWithCompatibility(%q) model = %q, want %q", tt.model, got, tt.want)
			}
		})
	}
}

func TestBedrockExecute_RequestsCorrectEndpointAndModel(t *testing.T) {
	var (
		calledPath  string
		calledModel string
	)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		calledModel = gjson.GetBytes(b, "model").String()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"us.anthropic.claude-3-7-sonnet-20250219-v1:0","choices":[],"usage":{}}`))
	}))
	defer ts.Close()

	base := NewBaseExecutor()
	bedrock := NewBedrockExecutor(base)
	req := &Request{
		Model:   "bedrock/us.anthropic.claude-3-7-sonnet-20250219-v1:0",
		BaseURL: ts.URL + "/v1",
		APIKey:  "fake-key",
		Body:    mustJSON(map[string]any{"model": "bedrock/us.anthropic.claude-3-7-sonnet-20250219-v1:0", "messages": []any{map[string]string{"role": "user", "content": "hi"}}}),
	}

	resp, err := bedrock.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	if calledPath != "/v1/chat/completions" {
		t.Fatalf("expected path %q, got %q", "/v1/chat/completions", calledPath)
	}
	if calledModel != "us.anthropic.claude-3-7-sonnet-20250219-v1:0" {
		t.Fatalf("expected upstream model stripped, got %q", calledModel)
	}
}

