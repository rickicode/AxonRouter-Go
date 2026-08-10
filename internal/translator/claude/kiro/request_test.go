package kiro

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConvertClaudeRequestToKiro_TextMessage(t *testing.T) {
	body := []byte(`{
		"model": "claude-sonnet-4-6",
		"max_tokens": 4096,
		"temperature": 0.5,
		"system": "You are helpful.",
		"messages": [
			{"role": "user", "content": "Hello Claude Kiro text"}
		],
		"tools": [
			{"name": "get_weather", "description": "weather", "input_schema": {"type": "object", "properties": {"city": {"type": "string"}}}}
		]
	}`)

	out := ConvertClaudeRequestToKiro("claude-sonnet-4-6", body, true)
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
	if !strings.Contains(content, "Hello Claude Kiro text") {
		t.Errorf("current content missing user text: %q", content)
	}
	if !strings.Contains(content, "[Context: Current time is") {
		t.Errorf("context timestamp missing")
	}
	if !strings.Contains(content, "<instructions>") {
		t.Errorf("system instructions missing: %q", content)
	}
	if uim["modelId"] != "claude-sonnet-4.6" {
		t.Errorf("modelId = %v, want claude-sonnet-4.6", uim["modelId"])
	}

	history, _ := cs["history"].([]any)
	if len(history) != 0 {
		t.Errorf("history expected empty when system folds into current turn, got %d", len(history))
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

func TestConvertClaudeRequestToKiro_ToolUse(t *testing.T) {
	body := []byte(`{
		"model": "claude-sonnet-4-6",
		"max_tokens": 4096,
		"messages": [
			{"role": "user", "content": "get weather?"},
			{"role": "assistant", "content": [
				{"type": "tool_use", "id": "tu_1", "name": "get_weather", "input": {"city": "X"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "tu_1", "content": "sunny"}
			]}
		]
	}`)

	out := ConvertClaudeRequestToKiro("claude-sonnet-4-6", body, false)
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	cs, ok := payload["conversationState"].(map[string]any)
	if !ok {
		t.Fatalf("conversationState missing")
	}

	var toolUses []any
	history, _ := cs["history"].([]any)
	for _, item := range history {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		arm, ok := m["assistantResponseMessage"].(map[string]any)
		if !ok {
			continue
		}
		if u, ok := arm["toolUses"].([]any); ok {
			toolUses = append(toolUses, u...)
		}
	}
	if len(toolUses) != 1 {
		t.Fatalf("expected 1 toolUse in history, got %d", len(toolUses))
	}
	use := toolUses[0].(map[string]any)
	if use["name"] != "get_weather" {
		t.Errorf("toolUse name = %v, want get_weather", use["name"])
	}
	if use["toolUseId"] != "tu_1" {
		t.Errorf("toolUse id = %v, want tu_1", use["toolUseId"])
	}

	current, ok := cs["currentMessage"].(map[string]any)
	if !ok {
		t.Fatalf("currentMessage missing")
	}
	uim, ok := current["userInputMessage"].(map[string]any)
	if !ok {
		t.Fatalf("currentMessage missing userInputMessage")
	}
	ctx, _ := uim["userInputMessageContext"].(map[string]any)
	if ctx == nil {
		t.Fatalf("currentMessage missing userInputMessageContext with tool results")
	}
	results, _ := ctx["toolResults"].([]any)
	if len(results) != 1 {
		t.Errorf("expected 1 toolResult in current message, got %d", len(results))
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
