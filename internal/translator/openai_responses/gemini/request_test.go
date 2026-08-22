package gemini

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAIResponsesRequestToGemini_SystemInstruction(t *testing.T) {
	body := []byte(`{
		"model": "gemini-2.5-pro",
		"instructions": "You are a helpful assistant.",
		"input": [{"type": "message", "role": "user", "content": "hi"}]
	}`)
	out := ConvertOpenAIResponsesRequestToGemini("gemini-2.5-pro", body, false)
	if got := gjson.GetBytes(out, "systemInstruction.parts.0.text").String(); got != "You are a helpful assistant." {
		t.Fatalf("unexpected system text: %s", got)
	}
}

func TestConvertOpenAIResponsesRequestToGemini_InputString(t *testing.T) {
	body := []byte(`{"input": "hello"}`)
	out := ConvertOpenAIResponsesRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "contents.0.parts.0.text").String(); got != "hello" {
		t.Fatalf("expected input string coerced to user message, got: %s", got)
	}
}

func TestConvertOpenAIResponsesRequestToGemini_InputTextAndImage(t *testing.T) {
	body := []byte(`{
		"input": [{
			"type": "message",
			"role": "user",
			"content": [
				{"type": "input_text", "text": "look"},
				{"type": "input_image", "image_url": "data:image/png;base64,ABC123"}
			]
		}]
	}`)
	out := ConvertOpenAIResponsesRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "contents.0.parts.0.text").String(); got != "look" {
		t.Fatalf("unexpected text part: %s", got)
	}
	if got := gjson.GetBytes(out, "contents.0.parts.1.inlineData.mimeType").String(); got != "image/png" {
		t.Fatalf("unexpected image mime: %s", got)
	}
	if got := gjson.GetBytes(out, "contents.0.parts.1.inlineData.data").String(); got != "ABC123" {
		t.Fatalf("unexpected image data: %s", got)
	}
}

func TestConvertOpenAIResponsesRequestToGemini_ObjectImageURL(t *testing.T) {
	body := []byte(`{"input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":{"url":"data:image/webp;base64,ABC"}}]}]}`)
	out := ConvertOpenAIResponsesRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "contents.0.parts.0.inlineData.mimeType").String(); got != "image/webp" {
		t.Fatalf("unexpected object image mime: %s", got)
	}
	if got := gjson.GetBytes(out, "contents.0.parts.0.inlineData.data").String(); got != "ABC" {
		t.Fatalf("unexpected object image data: %s", got)
	}
}

func TestConvertOpenAIResponsesRequestToGemini_InputAudio(t *testing.T) {
	body := []byte(`{
		"input": [{
			"type": "message",
			"role": "user",
			"content": [{"type": "input_audio", "data": "data:audio/wav;base64,XXX", "format": "wav"}]
		}]
	}`)
	out := ConvertOpenAIResponsesRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "contents.0.parts.0.inlineData.mimeType").String(); got != "audio/wav" {
		t.Fatalf("unexpected audio mime: %s", got)
	}
	if got := gjson.GetBytes(out, "contents.0.parts.0.inlineData.data").String(); got != "XXX" {
		t.Fatalf("unexpected audio data: %s", got)
	}
}

func TestConvertOpenAIResponsesRequestToGemini_InputFileDataURL(t *testing.T) {
	body := []byte(`{
		"input": [{
			"type": "message",
			"role": "user",
			"content": [{"type": "input_file", "file_data": "data:application/pdf;base64,PDFFILE"}]
		}]
	}`)
	out := ConvertOpenAIResponsesRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "contents.0.parts.0.inlineData.mimeType").String(); got != "application/pdf" {
		t.Fatalf("unexpected file mime: %s", got)
	}
	if got := gjson.GetBytes(out, "contents.0.parts.0.inlineData.data").String(); got != "PDFFILE" {
		t.Fatalf("unexpected file data: %s", got)
	}
}

func TestConvertOpenAIResponsesRequestToGemini_FunctionCallAndOutput(t *testing.T) {
	body := []byte(`{
		"input": [
			{"type": "function_call", "call_id": "call_1", "name": "get_weather", "arguments": "{\"city\":\"Paris\"}"},
			{"type": "function_call_output", "call_id": "call_1", "output": "{\"temp\":20}"}
		]
	}`)
	out := ConvertOpenAIResponsesRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "contents.0.parts.0.functionCall.name").String(); got != "get_weather" {
		t.Fatalf("unexpected functionCall name: %s", got)
	}
	if got := gjson.GetBytes(out, "contents.0.parts.0.functionCall.args.city").String(); got != "Paris" {
		t.Fatalf("unexpected functionCall args: %s", got)
	}
	if got := gjson.GetBytes(out, "contents.1.parts.0.functionResponse.name").String(); got != "get_weather" {
		t.Fatalf("unexpected functionResponse name: %s", got)
	}
	if got := gjson.GetBytes(out, "contents.1.parts.0.functionResponse.response.result").String(); got != "{\"temp\":20}" {
		t.Fatalf("unexpected functionResponse result: %s", got)
	}
}

func TestConvertOpenAIResponsesRequestToGemini_ReasoningItem(t *testing.T) {
	body := []byte(`{
		"input": [{"type": "reasoning", "summary": [{"type": "summary_text", "text": "thinking..."}]}]
	}`)
	out := ConvertOpenAIResponsesRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "contents.0.role").String(); got != "model" {
		t.Fatalf("expected model role for reasoning item, got: %s", got)
	}
	if got := gjson.GetBytes(out, "contents.0.parts.0.text").String(); got != "thinking..." {
		t.Fatalf("unexpected reasoning text: %s", got)
	}
	if !gjson.GetBytes(out, "contents.0.parts.0.thought").Bool() {
		t.Fatalf("expected thought=true on reasoning part")
	}
}

func TestConvertOpenAIResponsesRequestToGemini_ToolsAsFunctionDeclarations(t *testing.T) {
	body := []byte(`{
		"input": [{"type": "message", "role": "user", "content": "hi"}],
		"tools": [
			{"type": "function", "name": "get_weather", "description": "weather", "parameters": {"type":"object","properties":{"city":{"type":"string"}}}, "strict": true}
		]
	}`)
	out := ConvertOpenAIResponsesRequestToGemini("gemini-test", body, false)
	decls := gjson.GetBytes(out, "tools.0.functionDeclarations").Array()
	if len(decls) != 1 {
		t.Fatalf("expected 1 function declaration, got %d", len(decls))
	}
	if got := decls[0].Get("name").String(); got != "get_weather" {
		t.Fatalf("unexpected declaration name: %s", got)
	}
	if got := decls[0].Get("parameters.type").String(); got != "object" {
		t.Fatalf("unexpected parameters.type: %s", got)
	}
	if !decls[0].Get("strict").Bool() {
		t.Fatalf("expected strict=true")
	}
}

func TestConvertOpenAIResponsesRequestToGemini_TextFormatJSONSchema(t *testing.T) {
	body := []byte(`{
		"input": [],
		"text": {"format": {"type": "json_schema", "schema": {"type":"object"}}}
	}`)
	out := ConvertOpenAIResponsesRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "generationConfig.responseMimeType").String(); got != "application/json" {
		t.Fatalf("unexpected responseMimeType: %s", got)
	}
	if got := gjson.GetBytes(out, "generationConfig.responseJsonSchema.type").String(); got != "object" {
		t.Fatalf("unexpected responseJsonSchema: %s", got)
	}
}

func TestConvertOpenAIResponsesRequestToGemini_ReasoningEffort(t *testing.T) {
	body := []byte(`{"input": [], "reasoning": {"effort": "high"}}`)
	out := ConvertOpenAIResponsesRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "generationConfig.thinkingConfig.thinkingLevel").String(); got != "high" {
		t.Fatalf("unexpected thinkingLevel: %s", got)
	}
	if !gjson.GetBytes(out, "generationConfig.thinkingConfig.includeThoughts").Bool() {
		t.Fatalf("expected includeThoughts=true")
	}
}

func TestConvertOpenAIResponsesRequestToGemini_SafetySettingsAttached(t *testing.T) {
	body := []byte(`{"input": [{"type": "message", "role": "user", "content": "hi"}]}`)
	out := ConvertOpenAIResponsesRequestToGemini("gemini-test", body, false)
	settings := gjson.GetBytes(out, "safetySettings").Array()
	if len(settings) == 0 {
		t.Fatalf("expected safety settings to be attached")
	}
}
