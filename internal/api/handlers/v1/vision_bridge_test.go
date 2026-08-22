package v1

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/tidwall/gjson"
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

func TestDisableVisionBridgeStreaming(t *testing.T) {
	body := []byte(`{"stream":true,"stream_options":{"include_usage":true},"messages":[]}`)
	out := disableVisionBridgeStreaming(body, executor.FormatOpenAI)
	if gjson.GetBytes(out, "stream").Exists() || gjson.GetBytes(out, "stream_options").Exists() {
		t.Fatalf("bridge request retained streaming fields: %s", out)
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

func TestBuildVisionPromptBody_PreservesStructuredSystemPrompts(t *testing.T) {
	tests := []struct {
		name   string
		format executor.ProviderFormat
		body   string
		path   string
	}{
		{name: "claude", format: executor.FormatClaude, body: `{"system":[{"type":"text","text":"keep me"}],"messages":[]}`, path: "system.0.text"},
		{name: "responses", format: executor.FormatOpenAIResponses, body: `{"instructions":[{"type":"input_text","text":"keep me"}],"input":[]}`, path: "instructions.0.text"},
		{name: "gemini", format: executor.FormatGemini, body: `{"systemInstruction":{"parts":[{"text":"keep me"}]},"contents":[]}`, path: "systemInstruction.parts.0.text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := buildVisionPromptBody([]byte(tt.body), tt.format)
			if got := gjson.GetBytes(out, tt.path).String(); got != "keep me" {
				t.Fatalf("existing system instruction lost: %q", got)
			}
			if !bytes.Contains(out, []byte(visionBridgeInstruction)) {
				t.Fatal("bridge instruction was not appended")
			}
		})
	}
}

func TestReplaceImagesWithDescription_PreservesImageOrder(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"one"}},{"type":"image_url","image_url":{"url":"two"}}]}]}`)
	out := replaceImagesWithDescription(body, executor.FormatOpenAI, "IMAGE 1: a cat\nIMAGE 2: a dog")
	first := gjson.GetBytes(out, "messages.0.content.0.text").String()
	second := gjson.GetBytes(out, "messages.0.content.1.text").String()
	if !strings.Contains(first, "a cat") || strings.Contains(first, "a dog") {
		t.Fatalf("first image got wrong description: %q", first)
	}
	if !strings.Contains(second, "a dog") || strings.Contains(second, "a cat") {
		t.Fatalf("second image got wrong description: %q", second)
	}
}

func TestReplaceImagesWithDescription_ResponsesTopLevelImage(t *testing.T) {
	body := []byte(`{"input":[{"type":"input_image","image_url":"data:image/png;base64,abc"}]}`)
	out := replaceImagesWithDescription(body, executor.FormatOpenAIResponses, "a chart")
	if got := gjson.GetBytes(out, "input.0.type").String(); got != "input_text" {
		t.Fatalf("top-level input image type = %q, want input_text", got)
	}
	if !strings.Contains(gjson.GetBytes(out, "input.0.text").String(), "a chart") {
		t.Fatalf("top-level input image was not replaced: %s", out)
	}
}

func TestModelSupportsVision(t *testing.T) {
	for _, test := range []struct {
		model string
		want  bool
	}{
		{model: "openai/gpt-4o", want: true},
		{model: "cx/gpt-5.4", want: true},
		{model: "claude/claude-sonnet-4-5-20250929", want: true},
		{model: "aistudio/gemini-2.5-pro", want: true},
		{model: "vertex/gemini-2.5-pro", want: true},
		{model: "cf/llava-hf/llava-1.5-7b-hf", want: true},
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

func TestExtractAssistantContent_Interactions(t *testing.T) {
	body := []byte(`{"steps":[{"type":"model_output","content":[{"type":"text","text":"IMAGE 1: a chart"}]}]}`)
	if got := extractAssistantContent(body); got != "IMAGE 1: a chart" {
		t.Fatalf("Interactions text = %q, want structured model output", got)
	}
}
