package v1

import (
	"encoding/json"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/executor"
)

func TestBuildVisionPromptBody(t *testing.T) {
	body := []byte(`{"model":"openai/gpt-3.5-turbo","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]}]}`)
	got := buildVisionPromptBody(body, executor.FormatOpenAI)

	var root map[string]any
	if err := json.Unmarshal(got, &root); err != nil {
		t.Fatalf("buildVisionPromptBody returned invalid JSON: %v", err)
	}
	messages, ok := root["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("expected instruction plus original message, got %#v", root["messages"])
	}
	instruction := messages[0].(map[string]any)
	if instruction["role"] != "system" || instruction["content"] != visionBridgeInstruction {
		t.Fatalf("unexpected instruction message: %#v", instruction)
	}
	original := messages[1].(map[string]any)
	if original["role"] != "user" {
		t.Fatalf("original message was not preserved: %#v", original)
	}
}

func TestReplaceImagesWithDescription(t *testing.T) {
	tests := []struct {
		name   string
		format executor.ProviderFormat
		body   string
		check  func(t *testing.T, body map[string]any)
	}{
		{
			name:   "openai chat",
			format: executor.FormatOpenAI,
			body:   `{"messages":[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"x"}}]}]}`,
			check: func(t *testing.T, root map[string]any) {
				parts := root["messages"].([]any)[0].(map[string]any)["content"].([]any)
				if parts[1].(map[string]any)["type"] != "text" {
					t.Fatalf("image block was not converted: %#v", parts[1])
				}
			},
		},
		{
			name:   "claude",
			format: executor.FormatClaude,
			body:   `{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"abc"}}]}]}`,
			check: func(t *testing.T, root map[string]any) {
				parts := root["messages"].([]any)[0].(map[string]any)["content"].([]any)
				if parts[0].(map[string]any)["type"] != "text" {
					t.Fatalf("Claude image block was not converted: %#v", parts[0])
				}
			},
		},
		{
			name:   "responses",
			format: executor.FormatOpenAIResponses,
			body:   `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"x"}]}]}`,
			check: func(t *testing.T, root map[string]any) {
				parts := root["input"].([]any)[0].(map[string]any)["content"].([]any)
				if parts[0].(map[string]any)["type"] != "input_text" {
					t.Fatalf("Responses image block was not converted: %#v", parts[0])
				}
			},
		},
		{
			name:   "gemini",
			format: executor.FormatGemini,
			body:   `{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":"abc"}}]}]}`,
			check: func(t *testing.T, root map[string]any) {
				parts := root["contents"].([]any)[0].(map[string]any)["parts"].([]any)
				if _, ok := parts[0].(map[string]any)["text"]; !ok {
					t.Fatalf("Gemini image part was not converted: %#v", parts[0])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := replaceImagesWithDescription([]byte(tt.body), tt.format, "a red car")
			if string(got) == tt.body {
				t.Fatal("expected image body to change")
			}
			var root map[string]any
			if err := json.Unmarshal(got, &root); err != nil {
				t.Fatalf("replacement returned invalid JSON: %v", err)
			}
			tt.check(t, root)
		})
	}
}

func TestModelSupportsVision(t *testing.T) {
	for _, test := range []struct {
		model string
		want  bool
	}{
		{model: "openai/gpt-4o", want: true},
		{model: "cx/gpt-5.4", want: true},
		{model: "openai/gpt-3.5-turbo", want: false},
		{model: "deepseek/deepseek-chat", want: false},
	} {
		t.Run(test.model, func(t *testing.T) {
			if got := modelSupportsVision(test.model); got != test.want {
				t.Fatalf("modelSupportsVision(%q) = %t, want %t", test.model, got, test.want)
			}
		})
	}
}
