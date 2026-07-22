package executor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
	"github.com/tidwall/gjson"
)

func TestBedrockResolveBaseURL(t *testing.T) {
	tests := []struct {
		name string
		base string
		psd  map[string]string
		want string
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
		name  string
		model string
		want  string
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

func TestNormalizeBedrockToolSchema(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
		want  map[string]any
	}{
		{
			name:  "empty schema becomes object with properties",
			input: map[string]any{},
			want:  map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			name:  "nil schema becomes object with properties",
			input: nil,
			want:  map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			name: "unsupported keywords removed",
			input: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
				"anyOf":                []any{map[string]any{"type": "string"}},
				"oneOf":                []any{},
				"allOf":                []any{},
				"not":                  map[string]any{},
				"$schema":              "http://json-schema.org/draft-07/schema#",
				"$id":                  "id",
				"$ref":                 "#/defs/ref",
				"$defs":                map[string]any{},
				"definitions":          map[string]any{},
			},
			want: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			name: "required filters out non-string items",
			input: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
				"required":   []any{"name", map[string]any{"x": "y"}},
			},
			want: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
				"required":   []any{"name"},
			},
		},
		{
			name: "required filters out missing properties",
			input: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
				"required":   []any{"name", "missing"},
			},
			want: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
				"required":   []any{"name"},
			},
		},
		{
			name: "nested object schemas normalized recursively",
			input: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"address": map[string]any{
						"type":                 "object",
						"properties":           map[string]any{"street": map[string]any{"type": "string"}},
						"required":             []any{"street", "missing"},
						"additionalProperties": false,
						"$schema":              "http://example.com/schema",
					},
				},
			},
			want: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"address": map[string]any{
						"type":       "object",
						"properties": map[string]any{"street": map[string]any{"type": "string"}},
						"required":   []any{"street"},
					},
				},
			},
		},
		{
			name: "array item schemas normalized recursively",
			input: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tags": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":                 "object",
							"properties":           map[string]any{"name": map[string]any{"type": "string"}},
							"required":             []any{"name", "missing"},
							"additionalProperties": false,
						},
					},
				},
			},
			want: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tags": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":       "object",
							"properties": map[string]any{"name": map[string]any{"type": "string"}},
							"required":   []any{"name"},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeBedrockToolSchema(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("normalizeBedrockToolSchema() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestNormalizeBedrockTools(t *testing.T) {
	input := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "weather",
				"description": "Get weather",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"city": map[string]any{"type": "string"}},
					"required":   []any{"city", "missing"},
					"anyOf":      []any{map[string]any{"type": "string"}},
				},
			},
		},
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "empty",
				"description": "No params",
			},
		},
	}

	got := normalizeBedrockTools(input)
	if len(got) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(got))
	}

	first := got[0].(map[string]any)["function"].(map[string]any)["parameters"].(map[string]any)
	wantFirst := map[string]any{
		"type":       "object",
		"properties": map[string]any{"city": map[string]any{"type": "string"}},
		"required":   []any{"city"},
	}
	if !reflect.DeepEqual(first, wantFirst) {
		t.Fatalf("first tool parameters = %#v, want %#v", first, wantFirst)
	}

	second := got[1].(map[string]any)["function"].(map[string]any)
	if second["name"] != "empty" {
		t.Fatalf("expected second tool name %q, got %q", "empty", second["name"])
	}
	secondParams, ok := second["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("expected second tool parameters to be set, got %#v", second["parameters"])
	}
	wantSecondParams := map[string]any{"type": "object", "properties": map[string]any{}}
	if !reflect.DeepEqual(secondParams, wantSecondParams) {
		t.Fatalf("second tool parameters = %#v, want %#v", secondParams, wantSecondParams)
	}
}

func TestBedrockPrepareRequest_NormalizesTools(t *testing.T) {
	bedrock := NewBedrockExecutor(NewBaseExecutor())
	body := mustJSON(map[string]any{
		"model": "bedrock/us.anthropic.claude-3-7-sonnet-20250219-v1:0",
		"messages": []any{
			map[string]string{"role": "user", "content": "call it"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "echo",
					"description": "echo",
					"parameters": map[string]any{
						"type":       "object",
						"properties": map[string]any{"msg": map[string]any{"type": "string"}},
						"required":   []any{"msg", "missing"},
						"anyOf":      []any{map[string]any{"type": "string"}},
					},
				},
			},
		},
	})

	req := &Request{
		Model:   "bedrock/us.anthropic.claude-3-7-sonnet-20250219-v1:0",
		BaseURL: "https://bedrock-mantle.us-west-2.api.aws/v1",
		Body:    body,
	}

	modified, err := bedrock.prepareRequest(req)
	if err != nil {
		t.Fatalf("prepareRequest error: %v", err)
	}

	if gjson.GetBytes(modified.Body, "model").String() != "us.anthropic.claude-3-7-sonnet-20250219-v1:0" {
		t.Fatalf("expected model prefix stripped, got %q", gjson.GetBytes(modified.Body, "model").String())
	}
	if gjson.GetBytes(modified.Body, "tools.0.function.name").String() != "echo" {
		t.Fatalf("expected tool name preserved")
	}
	if gjson.GetBytes(modified.Body, "tools.0.function.parameters.required.0").String() != "msg" {
		t.Fatalf("expected required to be filtered")
	}
	if gjson.GetBytes(modified.Body, "tools.0.function.parameters.required.1").Exists() {
		t.Fatalf("missing required should have been filtered")
	}
	if gjson.GetBytes(modified.Body, "tools.0.function.parameters.anyOf").Exists() {
		t.Fatalf("unsupported keyword anyOf should be removed")
	}
}
