package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestNormalizeAntigravityContents(t *testing.T) {
	inner := map[string]any{
		"contents": []any{
			map[string]any{
				"role": "model",
				"parts": []any{
					map[string]any{"text": "hello"},
					map[string]any{"functionResponse": map[string]any{"name": "foo", "response": map[string]any{"result": "ok"}}},
				},
			},
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{"text": ""},
					map[string]any{"text": "real"},
					map[string]any{"thought": true, "text": "hidden"},
					map[string]any{"thoughtSignature": "sig", "text": "cloaked"},
				},
			},
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{"text": "next"},
				},
			},
		},
	}

	normalizeAntigravityContents(inner)

	contents := inner["contents"].([]map[string]any)
	// All turns become role=user and merge into one consecutive block.
	if len(contents) != 1 {
		t.Fatalf("expected 1 merged turn, got %d", len(contents))
	}
	if contents[0]["role"] != "user" {
		t.Errorf("expected merged turn role=user, got %v", contents[0]["role"])
	}
	mergedParts := contents[0]["parts"].([]map[string]any)
	if len(mergedParts) != 4 { // hello + functionResponse + real + next
		t.Errorf("expected 5 merged parts, got %d: %+v", len(mergedParts), mergedParts)
	}
}

func TestNormalizeAntigravityContents_KeepsBypassSentinelOnToolCall(t *testing.T) {
	inner := map[string]any{
		"contents": []any{
			map[string]any{
				"role": "model",
				"parts": []any{
					map[string]any{
						"functionCall":     map[string]any{"name": "run_command"},
						"thoughtSignature": "skip_thought_signature_validator",
						"text":             "",
					},
				},
			},
		},
	}

	normalizeAntigravityContents(inner)

	contents := inner["contents"].([]map[string]any)
	if len(contents) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(contents))
	}
	parts := contents[0]["parts"].([]map[string]any)
	if len(parts) != 1 {
		t.Fatalf("expected tool-call part kept, got %d parts", len(parts))
	}
	if parts[0]["thoughtSignature"] != "skip_thought_signature_validator" {
		t.Errorf("expected bypass sentinel kept on tool-call turn")
	}
}

func TestNormalizeAntigravityContents_StripsEmptyFunctionCallWithoutName(t *testing.T) {
	inner := map[string]any{
		"contents": []any{
			map[string]any{
				"role": "model",
				"parts": []any{
					map[string]any{"functionCall": map[string]any{"name": ""}},
					map[string]any{"text": "hi"},
				},
			},
		},
	}

	normalizeAntigravityContents(inner)

	parts := inner["contents"].([]map[string]any)[0]["parts"].([]map[string]any)
	if len(parts) != 1 || parts[0]["text"] != "hi" {
		t.Errorf("expected empty-name functionCall dropped, got %+v", parts)
	}
}

func TestInjectToolConfig(t *testing.T) {
	inner := map[string]any{
		"tools": []any{
			map[string]any{"functionDeclarations": []any{map[string]any{"name": "foo"}}},
		},
	}
	injectToolConfig(inner)

	cfg, ok := inner["toolConfig"].(map[string]any)
	if !ok {
		t.Fatal("toolConfig missing")
	}
	mode := cfg["functionCallingConfig"].(map[string]any)["mode"].(string)
	if mode != "VALIDATED" {
		t.Errorf("expected VALIDATED mode, got %q", mode)
	}
}

func TestInjectToolConfig_NoTools(t *testing.T) {
	inner := map[string]any{"contents": []any{}}
	injectToolConfig(inner)
	if _, ok := inner["toolConfig"]; ok {
		t.Error("toolConfig should not be set when no tools")
	}
}

func TestEnvelopeUserAgent(t *testing.T) {
	cases := []struct {
		name     string
		data     map[string]string
		expected string
	}{
		{"gmail", map[string]string{"email": "foo@gmail.com"}, "antigravity"},
		{"googlemail", map[string]string{"email": "foo@googlemail.com"}, "antigravity"},
		{"enterprise", map[string]string{"email": "foo@corp.com"}, "jetski"},
		{"harness", map[string]string{"clientProfile": "harness"}, "jetski"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := &Request{ProviderSpecificData: tc.data}
			if got := envelopeUserAgent(req); got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestPickAntigravityProjectID(t *testing.T) {
	if got := pickAntigravityProjectID(map[string]any{"cloudaicompanionProject": "my-project"}); got != "my-project" {
		t.Errorf("expected plain string project, got %q", got)
	}
	if got := pickAntigravityProjectID(map[string]any{"cloudaicompanionProject": map[string]any{"id": "object-project"}}); got != "object-project" {
		t.Errorf("expected object project, got %q", got)
	}
	if got := pickAntigravityProjectID(map[string]any{}); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestWrapEnvelope_OmniRouteParity(t *testing.T) {
	e := NewAntigravityExecutor(NewBaseExecutor())
	ctx := context.Background()

	body, _ := json.Marshal(map[string]any{
		"contents": []any{
			map[string]any{
				"role": "user",
				"parts": []any{
					map[string]any{"text": "hi"},
				},
			},
		},
		"tools": []any{
			map[string]any{"functionDeclarations": []any{map[string]any{"name": "tool_a"}}},
		},
		"thinking":         map[string]any{"type": "adaptive"},
		"reasoning_effort": "high",
		"safetySettings":   []map[string]string{{"category": "HARM_CATEGORY_HARASSMENT", "threshold": "OFF"}},
	})

	req := &Request{
		Model: "gemini-2.5-pro",
		Body:  body,
		ProviderSpecificData: map[string]string{
			"projectId":     "proj-123",
			"clientProfile": "ide",
			"email":         "dev@example.com",
		},
	}

	out, err := e.wrapEnvelope(ctx, req)
	if err != nil {
		t.Fatalf("wrapEnvelope failed: %v", err)
	}

	root := gjson.ParseBytes(out)
	if root.Get("project").String() != "proj-123" {
		t.Errorf("expected project proj-123, got %s", root.Get("project").String())
	}
	if root.Get("userAgent").String() != "jetski" {
		t.Errorf("expected jetski userAgent for enterprise email, got %s", root.Get("userAgent").String())
	}
	if !root.Get("request.toolConfig.functionCallingConfig.mode").Exists() {
		t.Error("expected toolConfig to be injected")
	}
	if root.Get("request.toolConfig.functionCallingConfig.mode").String() != "VALIDATED" {
		t.Errorf("expected VALIDATED mode, got %s", root.Get("request.toolConfig.functionCallingConfig.mode").String())
	}
	if root.Get("request.thinking").Exists() || root.Get("request.reasoning_effort").Exists() {
		t.Error("expected stripped thinking fields")
	}
	// CLIProxyAPI strips request.safetySettings before sending the envelope; the
	// default safety settings live in the inner request during translation but are
	// removed by the executor wrapper.
	if root.Get("request.safetySettings").Exists() {
		t.Error("expected request.safetySettings to be stripped by wrapEnvelope")
	}
}

func TestResolveAntigravityModelID(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"gemini-3.1-pro", "gemini-pro-agent"},
		{"gemini-3-pro-preview", "gemini-pro-agent"}, // chain: preview -> 3.1-pro -> pro-agent
		{"gemini-3-pro-image-preview", "gemini-3-pro-image"},
		{"gemini-3-flash", "gemini-3-flash"},
		{"claude-sonnet-4-6", "claude-sonnet-4-6"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			if got := resolveAntigravityModelID(tc.input); got != tc.expected {
				t.Errorf("resolveAntigravityModelID(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestWrapEnvelope_ResolvesModelAlias(t *testing.T) {
	e := NewAntigravityExecutor(NewBaseExecutor())
	ctx := context.Background()

	body, _ := json.Marshal(map[string]any{
		"contents": []any{
			map[string]any{
				"role": "user",
				"parts": []any{map[string]any{"text": "hi"}},
			},
		},
	})

	req := &Request{
		Model:                "gemini-3.1-pro", // public alias
		Body:                 body,
		ProviderSpecificData: map[string]string{"projectId": "proj-1"},
	}

	out, err := e.wrapEnvelope(ctx, req)
	if err != nil {
		t.Fatalf("wrapEnvelope failed: %v", err)
	}
	root := gjson.ParseBytes(out)
	if got := root.Get("model").String(); got != "gemini-pro-agent" {
		t.Errorf("expected upstream model gemini-pro-agent, got %q", got)
	}
}

func TestAntigravityProFallbackChains(t *testing.T) {
	if got := antigravityProFallbackChains["gemini-3.1-pro-high"]; len(got) == 0 {
		t.Error("expected fallback chain for gemini-3.1-pro-high")
	}
	if got := antigravityProFallbackChains["gemini-3.1-pro-low"]; len(got) == 0 {
		t.Error("expected fallback chain for gemini-3.1-pro-low")
	}
}

func TestExecute_ProFallbackChainRetriesOn400(t *testing.T) {
	requests := 0
	expectedModels := []string{"gemini-3.1-pro-high", "gemini-pro-agent"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		model := gjson.GetBytes(body, "model").String()
		if requests >= len(expectedModels) {
			t.Errorf("unexpected extra request model=%q", model)
			http.Error(w, "too many attempts", http.StatusInternalServerError)
			return
		}
		if model != expectedModels[requests] {
			t.Errorf("request %d: expected model %q, got %q", requests, expectedModels[requests], model)
		}
		requests++
		if requests < 2 {
			http.Error(w, `{"error":{"message":"invalid model"}}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	be := NewBaseExecutor()
	e := NewAntigravityExecutor(be)
	req := &Request{
		Model:                "gemini-3.1-pro-high",
		BaseURL:              server.URL,
		Body:                 []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`),
		AccessToken:          "token",
		ProviderSpecificData: map[string]string{"projectId": "proj-1"},
	}
	_, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if requests != len(expectedModels) {
		t.Errorf("expected %d requests, got %d", len(expectedModels), requests)
	}
}

func TestAntigravity_NormalizesToolKeys(t *testing.T) {
	e := NewAntigravityExecutor(NewBaseExecutor())
	ctx := context.Background()

	body, _ := json.Marshal(map[string]any{
		"tools": []any{
			map[string]any{
				"functionDeclarations": []any{
					map[string]any{
						"name":        "get_weather",
						"description": "Get the weather",
						"parametersJsonSchema": map[string]any{
							"$schema":    "http://json-schema.org/draft-07/schema#",
							"type":       "object",
							"title":      "weather",
							"format":     "foo",
							"default":    map[string]any{},
							"x-provider": "openai",
							"properties": map[string]any{
								"location": map[string]any{
									"type":          "string",
									"propertyNames": true,
									"minLength":     1,
								},
							},
							"required":      []string{"location"},
							"propertyNames": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
		"contents": []any{
			map[string]any{"role": "user", "parts": []any{map[string]any{"text": "hi"}}},
		},
	})

	req := &Request{
		Model:                "gemini-pro-agent",
		Body:                 body,
		ProviderSpecificData: map[string]string{"projectId": "proj-1"},
	}

	out, err := e.wrapEnvelope(ctx, req)
	if err != nil {
		t.Fatalf("wrapEnvelope failed: %v", err)
	}
	root := gjson.ParseBytes(out)

	if root.Get("request.tools.0.functionDeclarations").Exists() {
		t.Error("expected functionDeclarations to be renamed to function_declarations")
	}
	if !root.Get("request.tools.0.function_declarations").Exists() {
		t.Error("expected function_declarations field after normalize")
	}
	if root.Get("request.tools.0.function_declarations.0.parametersJsonSchema").Exists() {
		t.Error("expected parametersJsonSchema to be renamed to parameters")
	}
	if !root.Get("request.tools.0.function_declarations.0.parameters").Exists() {
		t.Error("expected parameters field after normalize")
	}
	if got := root.Get("request.tools.0.function_declarations.0.parameters.type").String(); got != "object" {
		t.Errorf("expected parameters.type = object, got %q", got)
	}
	for _, bad := range []string{"$schema", "format", "default", "x-provider", "propertyNames", "minLength"} {
		if root.Get("request.tools.0.function_declarations.0.parameters." + bad).Exists() {
			t.Errorf("expected unsupported key %q to be stripped", bad)
		}
	}
	// Constraints under property schemas are preserved as description hints.
	if !strings.Contains(root.Get("request.tools.0.function_declarations.0.parameters.properties.location.description").String(), "minLength: 1") {
		t.Errorf("expected minLength to be moved to description hint")
	}
	if got := root.Get("request.tools.0.function_declarations.0.parameters.properties.location.type").String(); got != "string" {
		t.Errorf("expected location type to remain, got %q", got)
	}
}

func TestCleanJSONSchemaForAntigravity_RemovesUnsupportedKeywords(t *testing.T) {
	input := `{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"$id": "root-schema",
		"type": "object",
		"propertyNames": {"type": "string"},
		"patternProperties": {"^x-": {"type": "string"}},
		"x-google-enum-descriptions": ["foo"],
		"properties": {
			"url": {"type": "string", "format": "uri", "default": "https://example.com"},
			"tags": {"type": "array", "minItems": 1, "uniqueItems": true}
		}
	}`

	result := CleanJSONSchemaForAntigravity(input)

	for _, key := range []string{"$schema", "$id", "propertyNames", "patternProperties", "format", "default", "uniqueItems", "minItems"} {
		if strings.Contains(result, fmt.Sprintf("\"%s\"", key)) {
			t.Errorf("expected %q to be removed, got: %s", key, result)
		}
	}
	if strings.Contains(result, "x-google-enum-descriptions") {
		t.Error("expected x-* extension field to be removed")
	}
	if !strings.Contains(result, "format: uri") {
		t.Error("expected format hint in description")
	}
	if !strings.Contains(result, "minItems: 1") {
		t.Error("expected minItems hint in description")
	}
}

func TestCleanJSONSchemaForAntigravity_ConvertsRefToHint(t *testing.T) {
	input := `{
		"definitions": {"User": {"type": "object", "properties": {"name": {"type": "string"}}}},
		"type": "object",
		"properties": {
			"customer": {"$ref": "#/definitions/User"}
		}
	}`

	result := CleanJSONSchemaForAntigravity(input)

	parsed := gjson.Parse(result)
	if prop := parsed.Get("properties.customer"); !prop.Exists() {
		t.Fatal("customer property missing")
	} else {
		if prop.Get("$ref").Exists() {
			t.Error("expected $ref to be removed")
		}
		desc := prop.Get("description").String()
		if !strings.Contains(desc, "See: User") {
			t.Errorf("expected ref hint, got description %q", desc)
		}
	}
}

func TestCleanJSONSchemaForAntigravity_ConvertsConstToEnum(t *testing.T) {
	input := `{
		"type": "object",
		"properties": {
			"kind": {"type": "string", "const": "InsightVizNode"}
		}
	}`

	result := CleanJSONSchemaForAntigravity(input)

	parsed := gjson.Parse(result)
	enum := parsed.Get("properties.kind.enum").Array()
	if len(enum) != 1 || enum[0].String() != "InsightVizNode" {
		t.Errorf("expected const converted to enum, got %s", parsed.Get("properties.kind").Raw)
	}
}

func TestCleanJSONSchemaForAntigravity_FlattensNullableTypeArray(t *testing.T) {
	input := `{
		"type": "object",
		"properties": {
			"name": {"type": ["string", "null"]}
		},
		"required": ["name"]
	}`

	result := CleanJSONSchemaForAntigravity(input)

	parsed := gjson.Parse(result)
	if got := parsed.Get("properties.name.type").String(); got != "string" {
		t.Errorf("expected type flattened to string, got %q", got)
	}
	if !strings.Contains(parsed.Get("properties.name.description").String(), "(nullable)") {
		t.Error("expected nullable hint")
	}
	if parsed.Get("required").Exists() {
		t.Error("expected nullable property to be removed from required")
	}
}
