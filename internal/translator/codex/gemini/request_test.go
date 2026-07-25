package gemini

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertCodexRequestToGemini_SystemInstructionFromInstructions(t *testing.T) {
	body := []byte(`{
		"model": "gemini-test",
		"instructions": "You are a helpful assistant.",
		"input": [{"type": "message", "role": "user", "content": "hi"}]
	}`)
	out := convertCodexRequestToGemini("gemini-test", body, false)
	if !gjson.GetBytes(out, "systemInstruction").Exists() {
		t.Fatalf("expected systemInstruction")
	}
	if got := gjson.GetBytes(out, "systemInstruction.parts.0.text").String(); got != "You are a helpful assistant." {
		t.Fatalf("unexpected system text: %s", got)
	}
}

func TestConvertCodexRequestToGemini_SystemInstructionFromTopLevelSystem(t *testing.T) {
	body := []byte(`{
		"system": "SYS",
		"input": [{"type": "message", "role": "user", "content": "hi"}]
	}`)
	out := convertCodexRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "systemInstruction.parts.0.text").String(); got != "SYS" {
		t.Fatalf("unexpected system text: %s", got)
	}
}

func TestConvertCodexRequestToGemini_InputStringCoercion(t *testing.T) {
	body := []byte(`{"input": "hello"}`)
	out := convertCodexRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "contents.0.parts.0.text").String(); got != "hello" {
		t.Fatalf("expected input string coerced to user message, got: %s", got)
	}
}

func TestConvertCodexRequestToGemini_ImageInlineDataStripsDataURI(t *testing.T) {
	body := []byte(`{
		"input": [{"type": "message", "role": "user", "content": [
			{"type": "input_image", "image_url": "data:image/png;base64,ABC123"}
		]}]
	}`)
	out := convertCodexRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "contents.0.parts.0.inlineData.mimeType").String(); got != "image/png" {
		t.Fatalf("unexpected mime: %s", got)
	}
	if got := gjson.GetBytes(out, "contents.0.parts.0.inlineData.data").String(); got != "ABC123" {
		t.Fatalf("unexpected data: %s", got)
	}
}

func TestConvertCodexRequestToGemini_ImageWithoutPrefixDefaultsToJPEG(t *testing.T) {
	body := []byte(`{
		"input": [{"type": "message", "role": "user", "content": [
			{"type": "input_image", "image_url": "RAWBASE64"}
		]}]
	}`)
	out := convertCodexRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "contents.0.parts.0.inlineData.mimeType").String(); got != "image/jpeg" {
		t.Fatalf("unexpected default mime: %s", got)
	}
	if got := gjson.GetBytes(out, "contents.0.parts.0.inlineData.data").String(); got != "RAWBASE64" {
		t.Fatalf("unexpected data: %s", got)
	}
}

func TestConvertCodexRequestToGemini_FunctionCallInput(t *testing.T) {
	body := []byte(`{
		"input": [{"type": "function_call", "call_id": "call_1", "name": "get_weather", "arguments": "{\"city\":\"Paris\"}"}]
	}`)
	out := convertCodexRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "contents.0.parts.0.functionCall.name").String(); got != "get_weather" {
		t.Fatalf("unexpected function call name: %s", got)
	}
	if got := gjson.GetBytes(out, "contents.0.parts.0.functionCall.args.city").String(); got != "Paris" {
		t.Fatalf("unexpected function call args: %s", got)
	}
}

func TestConvertCodexRequestToGemini_FunctionCallOutputResolvesName(t *testing.T) {
	body := []byte(`{
		"input": [
			{"type": "function_call", "call_id": "call_1", "name": "get_weather", "arguments": "{}"},
			{"type": "function_call_output", "call_id": "call_1", "output": "{\"temp\":20}"}
		]
	}`)
	out := convertCodexRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "contents.1.parts.0.functionResponse.name").String(); got != "get_weather" {
		t.Fatalf("expected functionResponse.name get_weather, got: %s", got)
	}
	if got := gjson.GetBytes(out, "contents.1.parts.0.functionResponse.response.result").String(); got != "{\"temp\":20}" {
		t.Fatalf("unexpected functionResponse result: %s", got)
	}
}

func TestConvertCodexRequestToGemini_ToolsAsFunctionDeclarations(t *testing.T) {
	body := []byte(`{
		"input": [{"type": "message", "role": "user", "content": "hi"}],
		"tools": [
			{"type": "web_search"},
			{"type": "function", "name": "get_weather", "description": "weather", "parameters": {"type":"object","properties":{"city":{"type":"string"}}}, "strict": true}
		]
	}`)
	out := convertCodexRequestToGemini("gemini-test", body, false)
	if !gjson.GetBytes(out, "tools").Exists() {
		t.Fatalf("expected tools")
	}
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

func TestConvertCodexRequestToGemini_ReasoningEffort(t *testing.T) {
	tests := []struct {
		name            string
		effort          string
		wantLevel       string
		wantBudget      *int64
		wantInclude     bool
		wantIncludePath string
	}{
		{
			name:        "explicit-high",
			effort:      "high",
			wantLevel:   "high",
			wantInclude: true,
		},
		{
			name:            "auto",
			effort:          "auto",
			wantBudget:      int64Ptr(-1),
			wantInclude:     true,
			wantIncludePath: "generationConfig.thinkingConfig.thinkingBudget",
		},
		{
			name:        "none",
			effort:      "none",
			wantInclude: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"input": [], "reasoning": {"effort": "` + tc.effort + `"}}`)
			out := convertCodexRequestToGemini("gemini-test", body, false)

			if tc.wantLevel != "" {
				if got := gjson.GetBytes(out, "generationConfig.thinkingConfig.thinkingLevel").String(); got != tc.wantLevel {
					t.Fatalf("thinkingLevel=%q, want %q", got, tc.wantLevel)
				}
			}
			if tc.wantBudget != nil {
				if got := gjson.GetBytes(out, tc.wantIncludePath).Int(); got != *tc.wantBudget {
					t.Fatalf("%s=%d, want %d", tc.wantIncludePath, got, *tc.wantBudget)
				}
			}
			if got := gjson.GetBytes(out, "generationConfig.thinkingConfig.includeThoughts").Bool(); got != tc.wantInclude {
				t.Fatalf("includeThoughts=%v, want %v", got, tc.wantInclude)
			}
		})
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}

func TestConvertCodexRequestToGemini_MaxTokensAndTemperature(t *testing.T) {
	body := []byte(`{"max_output_tokens": 1024, "temperature": 0.5, "top_p": 0.9, "input": []}`)
	out := convertCodexRequestToGemini("gemini-test", body, false)
	if got := gjson.GetBytes(out, "generationConfig.maxOutputTokens").Int(); got != 1024 {
		t.Fatalf("unexpected maxOutputTokens: %d", got)
	}
	if got := gjson.GetBytes(out, "generationConfig.temperature").Float(); got != 0.5 {
		t.Fatalf("unexpected temperature: %v", got)
	}
	if got := gjson.GetBytes(out, "generationConfig.topP").Float(); got != 0.9 {
		t.Fatalf("unexpected topP: %v", got)
	}
}

func TestParseInlineImage(t *testing.T) {
	mime, data := parseInlineImage("data:image/webp;base64,ZZZ")
	if mime != "image/webp" || data != "ZZZ" {
		t.Fatalf("unexpected parse: %s / %s", mime, data)
	}
	mime, data = parseInlineImage("data:audio/mp3;base64,XXX")
	if mime != "audio/mp3" || data != "XXX" {
		t.Fatalf("unexpected audio parse: %s / %s", mime, data)
	}
	mime, data = parseInlineImage(strings.TrimSpace("  rawb64  "))
	if mime != "image/jpeg" || data != "rawb64" {
		t.Fatalf("unexpected raw parse: %s / %s", mime, data)
	}
}
