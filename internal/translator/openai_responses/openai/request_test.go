package openai

import (
	"encoding/json"
	"testing"

	"github.com/tidwall/gjson"
)

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestConvertOpenAIResponsesRequestToOpenAI_Simple(t *testing.T) {
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
		"max_output_tokens": 256,
		"temperature":       0.5,
		"top_p":             0.9,
	})

	out := convertOpenAIResponsesRequestToOpenAI("gpt-4o", req, true)

	if got := gjson.GetBytes(out, "model").String(); got != "gpt-4o" {
		t.Errorf("model = %q, want gpt-4o", got)
	}
	if got := gjson.GetBytes(out, "stream").Bool(); !got {
		t.Errorf("stream = false, want true")
	}
	if got := gjson.GetBytes(out, "max_tokens").Int(); got != 256 {
		t.Errorf("max_tokens = %d, want 256", got)
	}
	if got := gjson.GetBytes(out, "temperature").Float(); got != 0.5 {
		t.Errorf("temperature = %v, want 0.5", got)
	}
	if got := gjson.GetBytes(out, "top_p").Float(); got != 0.9 {
		t.Errorf("top_p = %v, want 0.9", got)
	}

	msgs := gjson.GetBytes(out, "messages").Array()
	if len(msgs) != 2 {
		t.Fatalf("messages length = %d, want 2", len(msgs))
	}
	if msgs[0].Get("role").String() != "system" || msgs[0].Get("content").String() != "Be helpful." {
		t.Errorf("first message = %v, want system 'Be helpful.'", msgs[0].Raw)
	}
	if msgs[1].Get("role").String() != "user" {
		t.Errorf("second message role = %q, want user", msgs[1].Get("role").String())
	}
	if msgs[1].Get("content.0.type").String() != "text" || msgs[1].Get("content.0.text").String() != "Hello" {
		t.Errorf("second message content = %v, want text 'Hello'", msgs[1].Get("content").Raw)
	}
}

func TestConvertOpenAIResponsesRequestToOpenAI_Roles(t *testing.T) {
	req := mustMarshal(t, map[string]any{
		"input": []any{
			map[string]any{"type": "message", "role": "developer", "content": []any{map[string]any{"type": "input_text", "text": "dev"}}},
			map[string]any{"type": "message", "role": "system", "content": []any{map[string]any{"type": "input_text", "text": "sys"}}},
			map[string]any{"type": "message", "role": "user", "content": "hi"},
			map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "hey"}}},
		},
	})

	out := convertOpenAIResponsesRequestToOpenAI("gpt-4o", req, false)
	msgs := gjson.GetBytes(out, "messages").Array()
	if len(msgs) != 4 {
		t.Fatalf("messages length = %d, want 4", len(msgs))
	}
	want := []string{"system", "system", "user", "assistant"}
	for i, role := range want {
		if got := msgs[i].Get("role").String(); got != role {
			t.Errorf("message %d role = %q, want %q", i, got, role)
		}
	}
	if got := msgs[3].Get("content.0.type").String(); got != "text" {
		t.Errorf("assistant content type = %q, want text", got)
	}
}

func TestConvertOpenAIResponsesRequestToOpenAI_InputString(t *testing.T) {
	req := mustMarshal(t, map[string]any{
		"input": "tell me a joke",
	})
	out := convertOpenAIResponsesRequestToOpenAI("gpt-4o", req, false)

	msgs := gjson.GetBytes(out, "messages").Array()
	if len(msgs) != 1 {
		t.Fatalf("messages length = %d, want 1", len(msgs))
	}
	if msgs[0].Get("role").String() != "user" {
		t.Errorf("role = %q, want user", msgs[0].Get("role").String())
	}
	if msgs[0].Get("content").String() != "tell me a joke" {
		t.Errorf("content = %q, want 'tell me a joke'", msgs[0].Get("content").String())
	}
}

func TestConvertOpenAIResponsesRequestToOpenAI_Tools(t *testing.T) {
	req := mustMarshal(t, map[string]any{
		"input": []any{
			map[string]any{"type": "message", "role": "user", "content": "weather?"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "get_weather",
					"description": "Get the weather.",
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
		"tool_choice": "auto",
	})

	out := convertOpenAIResponsesRequestToOpenAI("gpt-4o", req, false)

	tools := gjson.GetBytes(out, "tools").Array()
	if len(tools) != 1 {
		t.Fatalf("tools length = %d, want 1", len(tools))
	}
	if got := tools[0].Get("type").String(); got != "function" {
		t.Errorf("tool type = %q, want function", got)
	}
	if got := tools[0].Get("function.name").String(); got != "get_weather" {
		t.Errorf("tool name = %q, want get_weather", got)
	}
	if got := tools[0].Get("function.parameters.type").String(); got != "object" {
		t.Errorf("parameters.type = %q, want object", got)
	}
	if got := gjson.GetBytes(out, "tool_choice").String(); got != "auto" {
		t.Errorf("tool_choice = %q, want auto", got)
	}
}

func TestConvertOpenAIResponsesRequestToOpenAI_ToolChoiceFunction(t *testing.T) {
	req := mustMarshal(t, map[string]any{
		"tools": []any{
			map[string]any{
				"type":       "function",
				"name":       "get_weather",
				"parameters": map[string]any{"type": "object"},
			},
		},
		"tool_choice": map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "get_weather"},
		},
	})

	out := convertOpenAIResponsesRequestToOpenAI("gpt-4o", req, false)

	if got := gjson.GetBytes(out, "tool_choice.type").String(); got != "function" {
		t.Errorf("tool_choice.type = %q, want function", got)
	}
	if got := gjson.GetBytes(out, "tool_choice.function.name").String(); got != "get_weather" {
		t.Errorf("tool_choice.function.name = %q, want get_weather", got)
	}
}

func TestConvertOpenAIResponsesRequestToOpenAI_FunctionCall(t *testing.T) {
	req := mustMarshal(t, map[string]any{
		"input": []any{
			map[string]any{
				"type":      "function_call",
				"call_id":   "call_1",
				"name":      "get_weather",
				"arguments": map[string]any{"location": "NYC"},
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output":  "sunny",
			},
		},
	})

	out := convertOpenAIResponsesRequestToOpenAI("gpt-4o", req, false)
	msgs := gjson.GetBytes(out, "messages").Array()
	if len(msgs) != 2 {
		t.Fatalf("messages length = %d, want 2", len(msgs))
	}

	if got := msgs[0].Get("role").String(); got != "assistant" {
		t.Errorf("first message role = %q, want assistant", got)
	}
	if got := msgs[0].Get("tool_calls.0.id").String(); got != "call_1" {
		t.Errorf("tool_call id = %q, want call_1", got)
	}
	if got := msgs[0].Get("tool_calls.0.function.name").String(); got != "get_weather" {
		t.Errorf("tool_call function.name = %q, want get_weather", got)
	}
	if got := msgs[0].Get("tool_calls.0.function.arguments").String(); got != `{"location":"NYC"}` {
		t.Errorf("tool_call function.arguments = %q, want {\"location\":\"NYC\"}", got)
	}

	if got := msgs[1].Get("role").String(); got != "tool" {
		t.Errorf("second message role = %q, want tool", got)
	}
	if got := msgs[1].Get("tool_call_id").String(); got != "call_1" {
		t.Errorf("tool_call_id = %q, want call_1", got)
	}
	if got := msgs[1].Get("content").String(); got != "sunny" {
		t.Errorf("tool content = %q, want sunny", got)
	}
}

func TestConvertOpenAIResponsesRequestToOpenAI_ImagePart(t *testing.T) {
	req := mustMarshal(t, map[string]any{
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{
						"type":      "input_image",
						"image_url": "https://example.com/img.png",
						"detail":    "high",
					},
				},
			},
		},
	})

	out := convertOpenAIResponsesRequestToOpenAI("gpt-4o", req, false)
	msgs := gjson.GetBytes(out, "messages").Array()
	if len(msgs) != 1 {
		t.Fatalf("messages length = %d, want 1", len(msgs))
	}
	if got := msgs[0].Get("content.0.type").String(); got != "image_url" {
		t.Errorf("content type = %q, want image_url", got)
	}
	if got := msgs[0].Get("content.0.image_url.url").String(); got != "https://example.com/img.png" {
		t.Errorf("image url = %q, want https://example.com/img.png", got)
	}
	if got := msgs[0].Get("content.0.image_url.detail").String(); got != "high" {
		t.Errorf("image detail = %q, want high", got)
	}
}

func TestConvertOpenAIResponsesRequestToOpenAI_NamespaceTools(t *testing.T) {
	req := mustMarshal(t, map[string]any{
		"tools": []any{
			map[string]any{
				"type": "namespace",
				"name": "weather_tools",
				"tools": []any{
					map[string]any{
						"type":       "function",
						"name":       "get_weather",
						"parameters": map[string]any{"type": "object"},
					},
				},
			},
		},
	})

	out := convertOpenAIResponsesRequestToOpenAI("gpt-4o", req, false)
	tools := gjson.GetBytes(out, "tools").Array()
	if len(tools) != 1 {
		t.Fatalf("tools length = %d, want 1", len(tools))
	}
	if got := tools[0].Get("function.name").String(); got != "weather_tools__get_weather" {
		t.Errorf("namespaced tool name = %q, want weather_tools__get_weather", got)
	}
}
