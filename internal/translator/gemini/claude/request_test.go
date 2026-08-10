package claude

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertGeminiRequestToClaude_ImageInlineDataBecomesImageBlock(t *testing.T) {
	body := []byte(`{
		"contents": [
			{
				"role": "user",
				"parts": [
					{"text": "What is this?"},
					{"inlineData": {"mimeType": "image/png", "data": "ABC123"}}
				]
			}
		]
	}`)

	out := convertGeminiRequestToClaude("claude-test", body, false)
	root := gjson.ParseBytes(out)

	role := root.Get("messages.0.role").String()
	if role != "user" {
		t.Fatalf("expected role user, got %q", role)
	}

	img := root.Get("messages.0.content.1")
	if img.Get("type").String() != "image" {
		t.Fatalf("expected image block, got %q", img.Get("type").String())
	}
	if img.Get("source.type").String() != "base64" {
		t.Fatalf("expected base64 source, got %q", img.Get("source.type").String())
	}
	if img.Get("source.media_type").String() != "image/png" {
		t.Fatalf("unexpected media_type: %q", img.Get("source.media_type").String())
	}
	if img.Get("source.data").String() != "ABC123" {
		t.Fatalf("unexpected data: %q", img.Get("source.data").String())
	}
}

func TestConvertGeminiRequestToClaude_ImageInlineDataDefaultsMimeType(t *testing.T) {
	body := []byte(`{
		"contents": [
			{
				"role": "user",
				"parts": [{"inlineData": {"data": "RAWBASE64"}}]
			}
		]
	}`)

	out := convertGeminiRequestToClaude("claude-test", body, false)
	root := gjson.ParseBytes(out)

	if got := root.Get("messages.0.content.0.source.media_type").String(); got != "image/jpeg" {
		t.Fatalf("expected default image/jpeg, got %q", got)
	}
	if got := root.Get("messages.0.content.0.source.data").String(); got != "RAWBASE64" {
		t.Fatalf("expected RAWBASE64 data, got %q", got)
	}
}

func TestConvertGeminiRequestToClaude_ReasoningSignaturePreserved(t *testing.T) {
	body := []byte(`{
		"contents": [
			{
				"role": "model",
				"parts": [
					{"text": "I will think step by step.", "thought": true, "thoughtSignature": "sig-abc"},
					{"text": "Final answer."}
				]
			}
		]
	}`)

	out := convertGeminiRequestToClaude("claude-test", body, false)
	root := gjson.ParseBytes(out)

	if got := root.Get("messages.0.role").String(); got != "assistant" {
		t.Fatalf("expected assistant role, got %q", got)
	}

	thinking := root.Get("messages.0.content.0")
	if thinking.Get("type").String() != "thinking" {
		t.Fatalf("expected thinking block, got %q", thinking.Get("type").String())
	}
	if thinking.Get("thinking").String() != "I will think step by step." {
		t.Fatalf("unexpected thinking text: %q", thinking.Get("thinking").String())
	}
	if thinking.Get("signature").String() != "sig-abc" {
		t.Fatalf("unexpected signature: %q", thinking.Get("signature").String())
	}

	text := root.Get("messages.0.content.1")
	if text.Get("type").String() != "text" {
		t.Fatalf("expected text block after thinking, got %q", text.Get("type").String())
	}
	if text.Get("text").String() != "Final answer." {
		t.Fatalf("unexpected text: %q", text.Get("text").String())
	}
}

func TestConvertGeminiRequestToClaude_ReasoningSignatureSnakeCase(t *testing.T) {
	body := []byte(`{
		"contents": [
			{
				"role": "model",
				"parts": [
					{"text": "Internal reasoning", "thought": true, "thought_signature": "sig-xyz"}
				]
			}
		]
	}`)

	out := convertGeminiRequestToClaude("claude-test", body, false)
	root := gjson.ParseBytes(out)

	if got := root.Get("messages.0.content.0.signature").String(); got != "sig-xyz" {
		t.Fatalf("expected signature from snake_case key, got %q", got)
	}
}

func TestConvertGeminiRequestToClaude_FunctionCallingConfigMapsToToolChoice(t *testing.T) {
	body := []byte(`{
		"contents": [{"role": "user", "parts": [{"text": "hello"}]}],
		"tools": [{"functionDeclarations": [{"name": "get_weather", "description": "weather"}]}],
		"toolConfig": {"functionCallingConfig": {"mode": "ANY", "allowedFunctionNames": ["get_weather"]}}
	}`)

	out := convertGeminiRequestToClaude("claude-test", body, false)
	root := gjson.ParseBytes(out)

	if got := root.Get("tool_choice.type").String(); got != "tool" {
		t.Fatalf("expected tool_choice.type tool, got %q", got)
	}
	if got := root.Get("tool_choice.name").String(); got != "get_weather" {
		t.Fatalf("expected tool_choice.name get_weather, got %q", got)
	}
}

func TestConvertGeminiRequestToClaude_AutoFunctionCallingConfig(t *testing.T) {
	body := []byte(`{
		"contents": [{"role": "user", "parts": [{"text": "hello"}]}],
		"tools": [{"functionDeclarations": [{"name": "get_weather", "description": "weather"}]}],
		"toolConfig": {"functionCallingConfig": {"mode": "AUTO"}}
	}`)

	out := convertGeminiRequestToClaude("claude-test", body, false)
	root := gjson.ParseBytes(out)

	if got := root.Get("tool_choice").String(); got != "auto" {
		t.Fatalf("expected tool_choice auto, got %q", got)
	}
}

func TestConvertGeminiRequestToClaude_AnyMultiAllowedFiltersTools(t *testing.T) {
	body := []byte(`{
		"contents": [{"role": "user", "parts": [{"text": "hello"}]}],
		"tools": [
			{"functionDeclarations": [
				{"name": "get_weather", "description": "weather"},
				{"name": "get_stock", "description": "stock"}
			]}
		],
		"toolConfig": {"functionCallingConfig": {"mode": "ANY", "allowedFunctionNames": ["get_weather"]}}
	}`)

	out := convertGeminiRequestToClaude("claude-test", body, false)
	root := gjson.ParseBytes(out)

	if got := root.Get("tool_choice.type").String(); got != "tool" {
		t.Fatalf("expected tool_choice.type tool, got %q", got)
	}
	if got := root.Get("tool_choice.name").String(); got != "get_weather" {
		t.Fatalf("expected tool_choice.name get_weather, got %q", got)
	}
	if got := root.Get("tools").Array(); len(got) != 1 {
		t.Fatalf("expected 1 tool after filtering, got %d", len(got))
	}
	if got := root.Get("tools.0.function.name").String(); got != "get_weather" {
		t.Fatalf("expected remaining tool get_weather, got %q", got)
	}
}

func TestConvertGeminiRequestToClaude_ValidatedModeFiltersTools(t *testing.T) {
	body := []byte(`{
		"contents": [{"role": "user", "parts": [{"text": "hello"}]}],
		"tools": [
			{"functionDeclarations": [
				{"name": "get_weather", "description": "weather"},
				{"name": "get_stock", "description": "stock"},
				{"name": "get_news", "description": "news"}
			]}
		],
		"toolConfig": {"functionCallingConfig": {"mode": "VALIDATED", "allowedFunctionNames": ["get_weather", "get_stock"]}}
	}`)

	out := convertGeminiRequestToClaude("claude-test", body, false)
	root := gjson.ParseBytes(out)

	if got := root.Get("tool_choice").String(); got != "auto" {
		t.Fatalf("expected tool_choice auto, got %q", got)
	}
	tools := root.Get("tools").Array()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools after filtering, got %d", len(tools))
	}
	seen := map[string]bool{}
	for _, t := range tools {
		seen[t.Get("function.name").String()] = true
	}
	if !seen["get_weather"] || !seen["get_stock"] || seen["get_news"] {
		t.Fatalf("tools not filtered to expected allowed names: %v", seen)
	}
}
