package claude

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
	"google.golang.org/protobuf/encoding/protowire"
)

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestConvertOpenAIResponsesRequestToClaude_Simple(t *testing.T) {
	req := mustMarshal(t, map[string]any{
		"instructions": "Be helpful.",
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "Hello"},
				},
			},
		},
		"max_output_tokens": 1024,
	})

	out := ConvertOpenAIResponsesRequestToClaude("claude-opus-4", req, true)

	if got := gjson.GetBytes(out, "model").String(); got != "claude-opus-4" {
		t.Errorf("model = %q, want claude-opus-4", got)
	}
	if got := gjson.GetBytes(out, "max_tokens").Int(); got != 1024 {
		t.Errorf("max_tokens = %d, want 1024", got)
	}
	if got := gjson.GetBytes(out, "stream").Bool(); !got {
		t.Errorf("stream = false, want true")
	}

	msgs := gjson.GetBytes(out, "messages").Array()
	if len(msgs) != 2 {
		t.Fatalf("messages length = %d, want 2", len(msgs))
	}
	if msgs[0].Get("role").String() != "user" || msgs[0].Get("content").String() != "Be helpful." {
		t.Errorf("first message = %v, want user content 'Be helpful.'", msgs[0].Raw)
	}
	if msgs[1].Get("role").String() != "user" {
		t.Errorf("second message role = %q, want user", msgs[1].Get("role").String())
	}
	if msgs[1].Get("content.0.type").String() != "text" || msgs[1].Get("content.0.text").String() != "Hello" {
		t.Errorf("second message content = %v, want text 'Hello'", msgs[1].Get("content").Raw)
	}
}

func TestConvertOpenAIResponsesRequestToClaude_SamplingParams(t *testing.T) {
	req := mustMarshal(t, map[string]any{
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "Hello"},
				},
			},
		},
		"temperature": 0.7,
		"top_p":       0.9,
	})

	out := ConvertOpenAIResponsesRequestToClaude("claude-opus-4", req, false)

	if got := gjson.GetBytes(out, "temperature").Float(); got != 0.7 {
		t.Errorf("temperature = %v, want 0.7", got)
	}
	if got := gjson.GetBytes(out, "top_p").Float(); got != 0.9 {
		t.Errorf("top_p = %v, want 0.9", got)
	}
}

func TestConvertOpenAIResponsesRequestToClaude_ReasoningEffort(t *testing.T) {
	cases := []struct {
		effort       string
		wantType     string
		wantBudget   int64
		wantNoBudget bool
	}{
		{"none", "disabled", 0, true},
		{"auto", "enabled", 0, true},
		{"low", "enabled", 1024, false},
		{"medium", "enabled", 8192, false},
		{"high", "enabled", 24576, false},
		{"2000", "enabled", 2000, false},
	}

	for _, tc := range cases {
		t.Run(tc.effort, func(t *testing.T) {
			req := mustMarshal(t, map[string]any{
				"reasoning": map[string]any{"effort": tc.effort},
			})
			out := ConvertOpenAIResponsesRequestToClaude("claude-opus-4", req, false)

			if got := gjson.GetBytes(out, "thinking.type").String(); got != tc.wantType {
				t.Errorf("thinking.type = %q, want %q", got, tc.wantType)
			}
			if tc.wantNoBudget {
				if gjson.GetBytes(out, "thinking.budget_tokens").Exists() {
					t.Errorf("expected thinking.budget_tokens to be absent for effort %q", tc.effort)
				}
			} else {
				if got := gjson.GetBytes(out, "thinking.budget_tokens").Int(); got != tc.wantBudget {
					t.Errorf("thinking.budget_tokens = %d, want %d", got, tc.wantBudget)
				}
			}
		})
	}
}

func TestConvertOpenAIResponsesRequestToClaude_ReasoningSignature(t *testing.T) {
	// Build a minimal valid strict Claude single-layer thinking signature.
	channelBlock := protowire.AppendTag(nil, 1, protowire.VarintType)
	channelBlock = protowire.AppendVarint(channelBlock, 11)
	container := protowire.AppendTag(nil, 1, protowire.BytesType)
	container = protowire.AppendBytes(container, channelBlock)
	payload := protowire.AppendTag(nil, 2, protowire.BytesType)
	payload = protowire.AppendBytes(payload, container)
	sig := base64.StdEncoding.EncodeToString(payload)

	req := mustMarshal(t, map[string]any{
		"input": []any{
			map[string]any{
				"type":             "reasoning",
				"encrypted_content": sig,
				"summary": []any{
					map[string]any{"type": "summary_text", "text": "Reasoning summary."},
				},
			},
		},
	})

	out := ConvertOpenAIResponsesRequestToClaude("claude-opus-4", req, false)

	if got := gjson.GetBytes(out, "messages.0.role").String(); got != "assistant" {
		t.Errorf("reasoning message role = %q, want assistant", got)
	}
	if got := gjson.GetBytes(out, "messages.0.content.0.type").String(); got != "thinking" {
		t.Errorf("reasoning content type = %q, want thinking", got)
	}
	if got := gjson.GetBytes(out, "messages.0.content.0.thinking").String(); got != "Reasoning summary." {
		t.Errorf("reasoning thinking text = %q, want 'Reasoning summary.'", got)
	}
	if got := gjson.GetBytes(out, "messages.0.content.0.signature").String(); got != sig {
		t.Errorf("reasoning signature not preserved")
	}
}

func TestConvertOpenAIResponsesRequestToClaude_FunctionCall(t *testing.T) {
	req := mustMarshal(t, map[string]any{
		"input": []any{
			map[string]any{
				"type":      "function_call",
				"call_id":   "abc123",
				"name":      "get_weather",
				"arguments": map[string]any{"location": "NYC"},
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "abc123",
				"output":  "sunny",
			},
		},
	})

	out := ConvertOpenAIResponsesRequestToClaude("claude-opus-4", req, false)
	msgs := gjson.GetBytes(out, "messages").Array()
	if len(msgs) != 2 {
		t.Fatalf("messages length = %d, want 2", len(msgs))
	}

	if msgs[0].Get("role").String() != "assistant" {
		t.Errorf("first message role = %q, want assistant", msgs[0].Get("role").String())
	}
	if msgs[0].Get("content.0.type").String() != "tool_use" {
		t.Errorf("first message content type = %q, want tool_use", msgs[0].Get("content.0.type").String())
	}
	if got := msgs[0].Get("content.0.id").String(); got != "toolu_abc123" {
		t.Errorf("tool_use id = %q, want toolu_abc123", got)
	}
	if got := msgs[0].Get("content.0.name").String(); got != "get_weather" {
		t.Errorf("tool_use name = %q, want get_weather", got)
	}
	if got := msgs[0].Get("content.0.input.location").String(); got != "NYC" {
		t.Errorf("tool_use input.location = %q, want NYC", got)
	}

	if msgs[1].Get("role").String() != "user" {
		t.Errorf("second message role = %q, want user", msgs[1].Get("role").String())
	}
	if msgs[1].Get("content.0.type").String() != "tool_result" {
		t.Errorf("second message content type = %q, want tool_result", msgs[1].Get("content.0.type").String())
	}
	if got := msgs[1].Get("content.0.tool_use_id").String(); got != "toolu_abc123" {
		t.Errorf("tool_result tool_use_id = %q, want toolu_abc123", got)
	}
	if got := msgs[1].Get("content.0.content").String(); got != "sunny" {
		t.Errorf("tool_result content = %q, want sunny", got)
	}
}

func TestConvertOpenAIResponsesRequestToClaude_Tools(t *testing.T) {
	req := mustMarshal(t, map[string]any{
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "get_weather",
					"description": "Get weather for a location.",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"location": map[string]any{"type": "string"},
						},
						"required": []any{"location"},
					},
				},
			},
		},
	})

	out := ConvertOpenAIResponsesRequestToClaude("claude-opus-4", req, false)

	tools := gjson.GetBytes(out, "tools").Array()
	if len(tools) != 1 {
		t.Fatalf("tools length = %d, want 1", len(tools))
	}
	if got := tools[0].Get("name").String(); got != "get_weather" {
		t.Errorf("tool name = %q, want get_weather", got)
	}
	if got := tools[0].Get("input_schema.type").String(); got != "object" {
		t.Errorf("input_schema.type = %q, want object", got)
	}
	if got := tools[0].Get("input_schema.properties.location.type").String(); got != "string" {
		t.Errorf("input_schema property type = %q, want string", got)
	}
}

func TestSanitizeClaudeToolID(t *testing.T) {
	if got := sanitizeClaudeToolID(""); !strings.HasPrefix(got, "toolu_") || len(got) != 6+24 {
		t.Errorf("empty id sanitization invalid: %q", got)
	}
	if got := sanitizeClaudeToolID("toolu_existing"); got != "toolu_existing" {
		t.Errorf("existing toolu_ id changed: %q", got)
	}
	if got := sanitizeClaudeToolID("myid"); got != "toolu_myid" {
		t.Errorf("plain id not prefixed: %q", got)
	}
}
