package claude

import (
	"encoding/json"
	"testing"

	"github.com/tidwall/gjson"
)

func mustMarshalReq(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestOpenAIToolCallsBecomeToolUse(t *testing.T) {
	body := []byte(`{
		"model": "m",
		"messages": [
			{"role": "assistant", "content": null, "tool_calls": [
				{"id": "call_1", "type": "function", "function": {"name": "calc", "arguments": "{\"a\":1}"}}
			]}
		]
	}`)
	out := convertOpenAIRequestToClaude("m", body, false)
	root := gjson.ParseBytes(out)
	part := root.Get("messages.0.content.0")
	if part.Get("type").String() != "tool_use" {
		t.Fatalf("content part type = %q, want tool_use", part.Get("type").String())
	}
	if part.Get("id").String() != "call_1" {
		t.Errorf("id = %q, want call_1", part.Get("id").String())
	}
	if part.Get("name").String() != "calc_cc" {
		t.Errorf("name = %q, want calc_cc (cloaked)", part.Get("name").String())
	}
}

func TestConvertOpenAIRequestToClaude_ReasoningEffortLegacy(t *testing.T) {
	cases := []struct {
		name         string
		effort       string
		wantType     string
		wantBudget   int64
		wantNoBudget bool
	}{
		{"none", "none", "disabled", 0, true},
		{"auto", "auto", "enabled", 0, true},
		{"low", "low", "enabled", 1024, false},
		{"medium", "medium", "enabled", 8192, false},
		{"high", "high", "enabled", 24576, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := mustMarshalReq(t, map[string]any{
				"model":           "claude-opus-4-20250514",
				"reasoning_effort": tc.effort,
				"messages":        []any{map[string]any{"role": "user", "content": "hi"}},
			})
			out := convertOpenAIRequestToClaude("claude-opus-4-20250514", body, false)
			root := gjson.ParseBytes(out)
			if got := root.Get("thinking.type").String(); got != tc.wantType {
				t.Errorf("thinking.type = %q, want %q", got, tc.wantType)
			}
			if tc.wantNoBudget {
				if root.Get("thinking.budget_tokens").Exists() {
					t.Errorf("expected thinking.budget_tokens to be absent for effort %q", tc.effort)
				}
			} else {
				if got := root.Get("thinking.budget_tokens").Int(); got != tc.wantBudget {
					t.Errorf("thinking.budget_tokens = %d, want %d", got, tc.wantBudget)
				}
			}
		})
	}
}

func TestConvertOpenAIRequestToClaude_ReasoningEffortAdaptive(t *testing.T) {
	cases := []struct {
		name           string
		effort         string
		wantType       string
		wantEffort     string
		wantNoEffort   bool
	}{
		{"none", "none", "disabled", "", true},
		{"auto", "auto", "adaptive", "", true},
		{"low", "low", "adaptive", "low", false},
		{"medium", "medium", "adaptive", "medium", false},
		{"high", "high", "adaptive", "high", false},
		{"max", "max", "adaptive", "max", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := mustMarshalReq(t, map[string]any{
				"model":           "claude-sonnet-4-6",
				"reasoning_effort": tc.effort,
				"messages":        []any{map[string]any{"role": "user", "content": "hi"}},
			})
			out := convertOpenAIRequestToClaude("claude-sonnet-4-6", body, false)
			root := gjson.ParseBytes(out)
			if got := root.Get("thinking.type").String(); got != tc.wantType {
				t.Errorf("thinking.type = %q, want %q", got, tc.wantType)
			}
			if tc.wantNoEffort {
				if root.Get("output_config.effort").Exists() {
					t.Errorf("expected output_config.effort to be absent for effort %q", tc.effort)
				}
			} else {
				if got := root.Get("output_config.effort").String(); got != tc.wantEffort {
					t.Errorf("output_config.effort = %q, want %q", got, tc.wantEffort)
				}
			}
		})
	}
}

func TestConvertOpenAIRequestToClaude_SystemCacheControlPreserved(t *testing.T) {
	body := mustMarshalReq(t, map[string]any{
		"model": "claude-test",
		"messages": []any{
			map[string]any{
				"role":          "system",
				"content":       "sys",
				"cache_control": map[string]any{"type": "ephemeral"},
			},
		},
	})
	out := convertOpenAIRequestToClaude("claude-test", body, false)
	root := gjson.ParseBytes(out)
	sys := root.Get("system")
	if !sys.IsArray() {
		t.Fatalf("system = %v, want array", sys.Raw)
	}
	if got := sys.Get("0.type").String(); got != "text" {
		t.Errorf("system.0.type = %q, want text", got)
	}
	if got := sys.Get("0.text").String(); got != "sys" {
		t.Errorf("system.0.text = %q, want sys", got)
	}
	if got := sys.Get("0.cache_control.type").String(); got != "ephemeral" {
		t.Errorf("system.0.cache_control.type = %q, want ephemeral", got)
	}
}

func TestConvertOpenAIRequestToClaude_SystemArrayCacheControlPreserved(t *testing.T) {
	body := mustMarshalReq(t, map[string]any{
		"model": "claude-test",
		"messages": []any{
			map[string]any{
				"role": "system",
				"content": []any{
					map[string]any{
						"type":          "text",
						"text":          "part1",
						"cache_control": map[string]any{"type": "ephemeral"},
					},
					map[string]any{"type": "text", "text": "part2"},
				},
				"cache_control": map[string]any{"type": "persistent"},
			},
		},
	})
	out := convertOpenAIRequestToClaude("claude-test", body, false)
	root := gjson.ParseBytes(out)
	if got := root.Get("system.0.cache_control.type").String(); got != "ephemeral" {
		t.Errorf("system.0.cache_control.type = %q, want ephemeral", got)
	}
	// Message-level cache_control applies to the last block when it does not
	// already carry part-level cache_control (matches CLIProxyAPI).
	if got := root.Get("system.1.cache_control.type").String(); got != "persistent" {
		t.Errorf("system.1.cache_control.type = %q, want persistent", got)
	}
}

func TestConvertOpenAIRequestToClaude_MessageCacheControlPreserved(t *testing.T) {
	body := mustMarshalReq(t, map[string]any{
		"model": "claude-test",
		"messages": []any{
			map[string]any{
				"role":          "user",
				"content":       "hello",
				"cache_control": map[string]any{"type": "ephemeral"},
			},
		},
	})
	out := convertOpenAIRequestToClaude("claude-test", body, false)
	root := gjson.ParseBytes(out)
	arr := root.Get("messages.0.content").Array()
	if len(arr) != 1 {
		t.Fatalf("content length = %d, want 1", len(arr))
	}
	last := arr[0]
	if got := last.Get("type").String(); got != "text" {
		t.Fatalf("last content type = %q, want text", got)
	}
	if got := last.Get("cache_control.type").String(); got != "ephemeral" {
		t.Errorf("last content cache_control.type = %q, want ephemeral", got)
	}
}

func TestConvertOpenAIRequestToClaude_ToolCacheControlPreserved(t *testing.T) {
	body := mustMarshalReq(t, map[string]any{
		"model": "claude-test",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "get_weather",
					"description": "weather",
					"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
				},
				"cache_control": map[string]any{"type": "ephemeral"},
			},
		},
	})
	out := convertOpenAIRequestToClaude("claude-test", body, false)
	root := gjson.ParseBytes(out)
	if got := root.Get("tools.0.name").String(); got != "get_weather_cc" {
		t.Errorf("tool name = %q, want get_weather_cc", got)
	}
	if got := root.Get("tools.0.cache_control.type").String(); got != "ephemeral" {
		t.Errorf("tool cache_control.type = %q, want ephemeral", got)
	}
}

func TestConvertOpenAIRequestToClaude_ToolChoiceObjectForm(t *testing.T) {
	body := mustMarshalReq(t, map[string]any{
		"model": "claude-test",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
		"tool_choice": map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "calc"},
		},
	})
	out := convertOpenAIRequestToClaude("claude-test", body, false)
	root := gjson.ParseBytes(out)
	if got := root.Get("tool_choice.type").String(); got != "tool" {
		t.Errorf("tool_choice.type = %q, want tool", got)
	}
	if got := root.Get("tool_choice.name").String(); got != "calc_cc" {
		t.Errorf("tool_choice.name = %q, want calc_cc", got)
	}
}

func TestConvertOpenAIRequestToClaude_ToolChoiceStringForms(t *testing.T) {
	tests := []struct {
		input    string
		wantType string
		wantSet  bool
	}{
		{"auto", "auto", true},
		{"required", "any", true},
		{"none", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			body := mustMarshalReq(t, map[string]any{
				"model":       "claude-test",
				"messages":    []any{map[string]any{"role": "user", "content": "hi"}},
				"tool_choice": tc.input,
			})
			out := convertOpenAIRequestToClaude("claude-test", body, false)
			root := gjson.ParseBytes(out)
			exists := root.Get("tool_choice").Exists()
			if exists != tc.wantSet {
				t.Fatalf("tool_choice exists = %v, want %v", exists, tc.wantSet)
			}
			if tc.wantSet {
				if got := root.Get("tool_choice.type").String(); got != tc.wantType {
					t.Errorf("tool_choice.type = %q, want %q", got, tc.wantType)
				}
			}
		})
	}
}
