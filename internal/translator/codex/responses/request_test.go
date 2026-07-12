package responses

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertSystemMessageToDeveloper(t *testing.T) {
	req := []byte(`{"messages":[{"role":"system","content":"You are helpful"},{"role":"user","content":"hi"}]}`)
	out := ConvertOpenAIRequestToCodex("cx/gpt-5.4", req, true)
	if !gjson.GetBytes(out, "input.0.role").Exists() {
		t.Fatal("missing first input item")
	}
	if got := gjson.GetBytes(out, "input.0.role").String(); got != "developer" {
		t.Fatalf("expected role developer, got %s", got)
	}
	if got := gjson.GetBytes(out, "input.0.content.0.text").String(); got != "You are helpful" {
		t.Fatalf("expected system text, got %s", got)
	}
	if got := gjson.GetBytes(out, "input.1.content.0.text").String(); got != "hi" {
		t.Fatalf("expected user text, got %s", got)
	}
}

func TestConvertInputStringToArray(t *testing.T) {
	req := []byte(`{"input":"hello","messages":[]}`)
	out := ConvertOpenAIRequestToCodex("cx/gpt-5.4", req, true)
	if gjson.GetBytes(out, "input").Type != gjson.JSON {
		t.Fatalf("expected input to be coerced to array")
	}
}

func TestStripsUnsupportedFields(t *testing.T) {
	req := []byte(`{"messages":[{"role":"user","content":"x"}],"temperature":0.5,"top_p":0.9,"max_tokens":100,"max_completion_tokens":200,"max_output_tokens":300,"user":"alice","truncation":"auto","context_management":{"compaction":"auto"}}`)
	out := ConvertOpenAIRequestToCodex("cx/gpt-5.4", req, true)
	bad := []string{"temperature", "top_p", "max_tokens", "max_completion_tokens", "max_output_tokens", "user", "truncation", "context_management"}
	for _, f := range bad {
		if gjson.GetBytes(out, f).Exists() {
			t.Errorf("expected field %s to be stripped, but it exists", f)
		}
	}
}

func TestReasoningDefaults(t *testing.T) {
	req := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	out := ConvertOpenAIRequestToCodex("cx/gpt-5.4", req, true)
	if got := gjson.GetBytes(out, "reasoning.effort").String(); got != "medium" {
		t.Fatalf("expected default reasoning effort medium, got %s", got)
	}
	if got := gjson.GetBytes(out, "reasoning.summary").String(); got != "auto" {
		t.Fatalf("expected reasoning.summary auto, got %s", got)
	}
	arr := gjson.GetBytes(out, "include").Array()
	if len(arr) != 1 || arr[0].String() != "reasoning.encrypted_content" {
		t.Fatalf("expected include reasoning.encrypted_content, got %v", arr)
	}
	if !gjson.GetBytes(out, "parallel_tool_calls").Bool() {
		t.Fatal("expected parallel_tool_calls true")
	}
}

func TestReasoningEffortFromRequest(t *testing.T) {
	req := []byte(`{"messages":[{"role":"user","content":"hi"}],"reasoning_effort":"high"}`)
	out := ConvertOpenAIRequestToCodex("cx/gpt-5.4", req, true)
	if got := gjson.GetBytes(out, "reasoning.effort").String(); got != "high" {
		t.Fatalf("expected reasoning effort high, got %s", got)
	}
}

func TestResponseFormatJsonSchema(t *testing.T) {
	req := []byte(`{"messages":[{"role":"user","content":"x"}],"response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":{"type":"object"}}}}`)
	out := ConvertOpenAIRequestToCodex("cx/gpt-5.4", req, true)
	if got := gjson.GetBytes(out, "text.format.type").String(); got != "json_schema" {
		t.Fatalf("expected text.format.type json_schema, got %s", got)
	}
	if got := gjson.GetBytes(out, "text.format.name").String(); got != "answer" {
		t.Fatalf("expected name answer, got %s", got)
	}
	if !gjson.GetBytes(out, "text.format.strict").Bool() {
		t.Fatal("expected strict true")
	}
}

func TestResponseFormatText(t *testing.T) {
	req := []byte(`{"messages":[{"role":"user","content":"x"}],"response_format":{"type":"text"}}`)
	out := ConvertOpenAIRequestToCodex("cx/gpt-5.4", req, true)
	if got := gjson.GetBytes(out, "text.format.type").String(); got != "text" {
		t.Fatalf("expected text.format.type text, got %s", got)
	}
}

func TestMultimodalContentParts(t *testing.T) {
	req := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"https://example.com/img.png"}},{"type":"input_audio","input_audio":{"data":"abc","format":"wav"}},{"type":"file","file":{"file_data":"data:pdf;base64,xyz","filename":"doc.pdf"}}]}]}`)
	out := ConvertOpenAIRequestToCodex("cx/gpt-5.4", req, true)
	parts := gjson.GetBytes(out, "input.0.content").Array()
	if len(parts) != 4 {
		t.Fatalf("expected 4 parts, got %d", len(parts))
	}
	if parts[1].Get("type").String() != "input_image" {
		t.Fatalf("expected input_image, got %s", parts[1].Get("type").String())
	}
	if parts[2].Get("type").String() != "input_audio" {
		t.Fatalf("expected input_audio, got %s", parts[2].Get("type").String())
	}
	if parts[3].Get("type").String() != "input_file" {
		t.Fatalf("expected input_file, got %s", parts[3].Get("type").String())
	}
}

func TestMultiTurnToolUse(t *testing.T) {
	req := []byte(`{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"LA\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"sunny"}],"tools":[{"type":"function","function":{"name":"get_weather","description":"weather","parameters":{"type":"object"}}}]}`)
	out := ConvertOpenAIRequestToCodex("cx/gpt-5.4", req, true)
	items := gjson.GetBytes(out, "input").Array()
	if len(items) != 2 {
		t.Fatalf("expected 2 input items, got %d", len(items))
	}
	if items[0].Get("type").String() != "function_call" {
		t.Fatalf("expected function_call item, got %s", items[0].Get("type").String())
	}
	if items[0].Get("call_id").String() != "call_1" {
		t.Fatalf("expected call_id call_1, got %s", items[0].Get("call_id").String())
	}
	if items[1].Get("type").String() != "function_call_output" {
		t.Fatalf("expected function_call_output, got %s", items[1].Get("type").String())
	}
	if items[1].Get("output").String() != "sunny" {
		t.Fatalf("expected output sunny, got %s", items[1].Get("output").String())
	}
}

func TestToolNameShortening(t *testing.T) {
	longName := strings.Repeat("a", 70)
	req := []byte(`{"messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"` + longName + `","description":"x","parameters":{"type":"object"}}}],"tool_choice":{"type":"function","function":{"name":"` + longName + `"}}}`)
	out := ConvertOpenAIRequestToCodex("cx/gpt-5.4", req, true)
	toolName := gjson.GetBytes(out, "tools.0.name").String()
	if len(toolName) > 64 {
		t.Fatalf("tool name not shortened: len=%d", len(toolName))
	}
	choiceName := gjson.GetBytes(out, "tool_choice.name").String()
	if choiceName != toolName {
		t.Fatalf("tool_choice name %s does not match tool name %s", choiceName, toolName)
	}
}

func TestToolChoiceStringPassthrough(t *testing.T) {
	req := []byte(`{"messages":[{"role":"user","content":"hi"}],"tool_choice":"auto"}`)
	out := ConvertOpenAIRequestToCodex("cx/gpt-5.4", req, true)
	if got := gjson.GetBytes(out, "tool_choice").String(); got != "auto" {
		t.Fatalf("expected tool_choice auto, got %s", got)
	}
}

func TestBuiltInWebSearchToolPassthrough(t *testing.T) {
	req := []byte(`{"messages":[{"role":"user","content":"search"}],"tools":[{"type":"web_search"}],"tool_choice":{"type":"web_search"}}`)
	out := ConvertOpenAIRequestToCodex("cx/gpt-5.4", req, true)
	if got := gjson.GetBytes(out, "tools.0.type").String(); got != "web_search" {
		t.Fatalf("expected web_search tool, got %s", got)
	}
	if got := gjson.GetBytes(out, "tool_choice.type").String(); got != "web_search" {
		t.Fatalf("expected web_search tool_choice, got %s", got)
	}
}

func TestToolOutputMultimodalArray(t *testing.T) {
	req := []byte(`{"messages":[{"role":"tool","tool_call_id":"call_1","content":[{"type":"text","text":"here"},{"type":"image_url","image_url":{"url":"https://example.com/img.png"}}]}]}`)
	out := ConvertOpenAIRequestToCodex("cx/gpt-5.4", req, true)
	output := gjson.GetBytes(out, "input.0.output").Array()
	if len(output) != 2 {
		t.Fatalf("expected 2 output parts, got %d", len(output))
	}
	if output[0].Get("type").String() != "input_text" {
		t.Fatalf("expected input_text, got %s", output[0].Get("type").String())
	}
}

func TestAssistantWithoutContentIsSkipped(t *testing.T) {
	req := []byte(`{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"fn","arguments":"{}"}}]}]}`)
	out := ConvertOpenAIRequestToCodex("cx/gpt-5.4", req, true)
	items := gjson.GetBytes(out, "input").Array()
	if len(items) != 1 {
		t.Fatalf("expected only function_call item, got %d", len(items))
	}
	if items[0].Get("type").String() != "function_call" {
		t.Fatalf("expected function_call, got %s", items[0].Get("type").String())
	}
}
