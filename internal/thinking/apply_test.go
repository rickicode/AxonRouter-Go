package thinking

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestApplyThinkingOverride_Claude(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-5"}`)
	out := ApplyThinkingOverride(body, 8192, "claude")
	if got := gjson.GetBytes(out, "thinking.type").String(); got != "enabled" {
		t.Errorf("type = %q, want enabled", got)
	}
	if got := int(gjson.GetBytes(out, "thinking.budget_tokens").Int()); got != 8192 {
		t.Errorf("budget_tokens = %d, want 8192", got)
	}
}

func TestApplyThinkingOverride_ClaudeDisable(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-5","thinking":{"type":"enabled","budget_tokens":4096}}`)
	out := ApplyThinkingOverride(body, 0, "claude")
	if got := gjson.GetBytes(out, "thinking.type").String(); got != "disabled" {
		t.Errorf("type = %q, want disabled", got)
	}
}

func TestApplyThinkingOverride_OpenAI(t *testing.T) {
	body := []byte(`{"model":"gpt-4o"}`)
	out := ApplyThinkingOverride(body, 4096, "openai")
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "medium" {
		t.Errorf("reasoning_effort = %q, want medium", got)
	}
}

func TestApplyThinkingOverride_OpenAIResponses(t *testing.T) {
	body := []byte(`{"model":"cx-model"}`)
	out := ApplyThinkingOverride(body, -1, "openai-responses")
	if got := gjson.GetBytes(out, "reasoning.effort").String(); got != "auto" {
		t.Errorf("reasoning.effort = %q, want auto", got)
	}
}

func TestApplyThinkingOverride_GeminiDisable(t *testing.T) {
	body := []byte(`{"model":"gemini-2.5-pro"}`)
	out := ApplyThinkingOverride(body, 0, "gemini")
	if got := int(gjson.GetBytes(out, "generationConfig.thinkingConfig.thinkingBudget").Int()); got != 0 {
		t.Errorf("thinkingBudget = %d, want 0", got)
	}
}

func TestApplyThinkingOverride_UnknownFormat(t *testing.T) {
	body := []byte(`{"model":"x"}`)
	out := ApplyThinkingOverride(body, 4096, "kiro")
	if string(out) != string(body) {
		t.Errorf("unknown format should be unchanged")
	}
}
