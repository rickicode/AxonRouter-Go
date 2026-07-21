package kiro

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeKiroModel(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"claude-sonnet-4-6", "claude-sonnet-4.6"},
		{"claude-opus-4-20250514", "claude-opus-4-20250514"},
		{"claude-haiku-4-5", "claude-haiku-4.5"},
		{"deepseek-chat", "deepseek-chat"},
	}
	for _, c := range cases {
		got := normalizeKiroModel(c.in)
		if got != c.want {
			t.Errorf("normalizeKiroModel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolveKiroEffort(t *testing.T) {
	cases := []struct {
		name string
		req  map[string]any
		want string
	}{
		{"reasoning_effort high", map[string]any{"reasoning_effort": "high"}, "high"},
		{"thinking enabled budget 40000", map[string]any{"thinking": map[string]any{"type": "enabled", "budget_tokens": float64(40000)}}, "high"},
		{"thinking adaptive", map[string]any{"thinking": map[string]any{"type": "adaptive"}}, "high"},
		{"output_config xhigh", map[string]any{"output_config": map[string]any{"effort": "xhigh"}}, "xhigh"},
		{"no reasoning", map[string]any{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveKiroEffort(c.req)
			if got != c.want {
				t.Errorf("resolveKiroEffort(...) = %q, want %q", got, c.want)
			}
		})
	}
}

func TestConvertOpenAIRequestToKiro_Basic(t *testing.T) {
	body := []byte(`{
		"model": "kiro/claude-sonnet-4-6",
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Hi"}
		],
		"max_tokens": 4096,
		"temperature": 0.5,
		"tools": [
			{"type": "function", "function": {"name": "get_weather", "description": "weather", "parameters": {"type": "object", "properties": {"city": {"type": "string"}}}}}
		]
	}`)

	out := ConvertOpenAIRequestToKiro("kiro/claude-sonnet-4-6", body, true)
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	cs, ok := payload["conversationState"].(map[string]any)
	if !ok {
		t.Fatalf("conversationState missing")
	}
	current, ok := cs["currentMessage"].(map[string]any)
	if !ok {
		t.Fatalf("currentMessage missing")
	}
	uim, ok := current["userInputMessage"].(map[string]any)
	if !ok {
		t.Fatalf("userInputMessage missing")
	}
	content := uim["content"].(string)
	if !strings.Contains(content, "Hi") {
		t.Errorf("current content missing user text: %q", content)
	}
	if !strings.Contains(content, "[Context: Current time is") {
		t.Errorf("context timestamp missing")
	}
	if uim["modelId"] != "claude-sonnet-4.6" {
		t.Errorf("modelId = %v, want claude-sonnet-4.6", uim["modelId"])
	}
	history, _ := cs["history"].([]any)
	if len(history) != 0 {
		t.Errorf("history expected empty when system folds into user current turn, got %d", len(history))
	}
	inf, _ := payload["inferenceConfig"].(map[string]any)
	if inf == nil {
		t.Fatalf("inferenceConfig missing")
	}
	if inf["maxTokens"] != float64(4096) {
		t.Errorf("maxTokens = %v", inf["maxTokens"])
	}
	if inf["temperature"] != 0.5 {
		t.Errorf("temperature = %v", inf["temperature"])
	}
	if !uimHasTools(uim) {
		t.Errorf("current message missing tools schema")
	}
}

func TestConvertOpenAIRequestToKiro_Reasoning(t *testing.T) {
	body := []byte(`{
		"model": "claude-sonnet-4.5",
		"messages": [{"role": "user", "content": "Think deeply"}],
		"reasoning_effort": "high",
		"max_tokens": 8192
	}`)

	out := ConvertOpenAIRequestToKiro("claude-sonnet-4.5", body, true)
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := payload["additionalModelRequestFields"]; ok {
		t.Errorf("additionalModelRequestFields should not be present in top-level payload")
	}

	current := payload["conversationState"].(map[string]any)["currentMessage"].(map[string]any)
	uim := current["userInputMessage"].(map[string]any)
	content := uim["content"].(string)
	if !strings.HasPrefix(content, "<thinking_mode>enabled</thinking_mode>") {
		t.Errorf("thinking directive missing: %s", content)
	}
	if !strings.Contains(content, "Think deeply") {
		t.Errorf("user content missing: %s", content)
	}
	inf, ok := payload["inferenceConfig"].(map[string]any)
	if !ok {
		t.Fatalf("inferenceConfig missing; maxTokens should be preserved")
	}
	if inf["maxTokens"] != float64(8192) {
		t.Errorf("maxTokens = %v, want 8192", inf["maxTokens"])
	}
}

func TestConvertOpenAIRequestToKiro_UnsupportedSuffix(t *testing.T) {
	body := []byte(`{"messages": [{"role": "user", "content": "hi"}]}`)
	out := ConvertOpenAIRequestToKiro("claude-opus-4[1m]", body, true)
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	errObj := payload["error"].(map[string]any)
	if !strings.Contains(errObj["message"].(string), "[1m]") {
		t.Errorf("expected [1m] rejection error, got %v", errObj)
	}
}

func uimHasTools(uim map[string]any) bool {
	ctx, _ := uim["userInputMessageContext"].(map[string]any)
	if ctx == nil {
		return false
	}
	tools, _ := ctx["tools"].([]any)
	return len(tools) > 0
}

func TestConvertOpenAIRequestToKiro_SystemPromptUsesInstructionsTag(t *testing.T) {
	body := []byte(`{
		"model": "kiro/claude-sonnet-4-6",
		"messages": [
			{"role": "system", "content": "Be concise."},
			{"role": "user", "content": "Hello"}
		]
	}`)

	out := ConvertOpenAIRequestToKiro("kiro/claude-sonnet-4-6", body, true)
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := payload["systemPrompt"]; ok {
		t.Errorf("systemPrompt top-level field should not be present")
	}

	current := payload["conversationState"].(map[string]any)["currentMessage"].(map[string]any)
	content := current["userInputMessage"].(map[string]any)["content"].(string)
	if !strings.Contains(content, "<instructions>\nBe concise.\n</instructions>") {
		t.Errorf("current content missing folded system instructions: %q", content)
	}
	if strings.Contains(content, "<system-reminder>") {
		t.Errorf("current content still uses old <system-reminder> tag: %q", content)
	}
}

func TestSupportsReasoning_NewClaudeModels(t *testing.T) {
	cases := map[string]bool{
		"claude-sonnet-4.5": true,
		"claude-sonnet-4":   true,
		"claude-haiku-4.5":  false,
		"auto":              false,
	}
	for model, want := range cases {
		if got := supportsReasoning(model); got != want {
			t.Errorf("supportsReasoning(%q) = %v, want %v", model, got, want)
		}
	}
}

func TestBuildKiroTools_DescriptionTruncation(t *testing.T) {
	longDesc := strings.Repeat("a", 10050)
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "long_desc_tool",
				"description": longDesc,
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			},
		},
	}
	out := buildKiroTools(tools)
	if len(out) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(out))
	}
	spec := out[0]["toolSpecification"].(map[string]any)
	got, _ := spec["description"].(string)
	if len(got) > 10000 {
		t.Errorf("description length = %d, want <= 10000", len(got))
	}
	if !strings.HasSuffix(got, " …") {
		t.Errorf("description should end with ellipsis marker, got suffix %q", got[len(got)-10:])
	}
}

func TestConvertOpenAIRequestToKiro_HTTPImageFallback(t *testing.T) {
	body := []byte(`{
		"model": "kiro/claude-sonnet-4-6",
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "describe this"},
				{"type": "image_url", "image_url": {"url": "https://example.com/pic.png"}}
			]},
			{"role": "assistant", "content": "ok"}
		]
	}`)

	out := ConvertOpenAIRequestToKiro("kiro/claude-sonnet-4-6", body, false)
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	cs := payload["conversationState"].(map[string]any)
	history, _ := cs["history"].([]any)
	if len(history) == 0 {
		t.Fatalf("expected history user message, got none")
	}
	uim := history[0].(map[string]any)["userInputMessage"].(map[string]any)
	content := uim["content"].(string)
	if !strings.Contains(content, "[Image: https://example.com/pic.png]") {
		t.Errorf("history user content missing HTTP image fallback, got %q", content)
	}
}

func TestConvertOpenAIRequestToKiro_Base64ImageStillInImages(t *testing.T) {
	body := []byte(`{
		"model": "kiro/claude-sonnet-4-6",
		"messages": [
			{"role": "user", "content": [
				{"type": "image_url", "image_url": {"url": "data:image/png;base64,aGVsbG8="}}
			]}
		]
	}`)

	out := ConvertOpenAIRequestToKiro("kiro/claude-sonnet-4-6", body, false)
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	cs := payload["conversationState"].(map[string]any)
	current := cs["currentMessage"].(map[string]any)["userInputMessage"].(map[string]any)
	images, _ := current["images"].([]any)
	if len(images) == 0 {
		t.Errorf("base64 image was not placed into images field")
	}
}
