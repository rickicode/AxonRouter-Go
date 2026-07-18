package grok_cli

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertSystemMessagePreservesRole(t *testing.T) {
	req := []byte(`{"messages":[{"role":"system","content":"You are helpful"},{"role":"user","content":"hi"}]}`)
	out := ConvertOpenAIRequestToGrokCLI("grok-cli/grok-4.3", req, true)

	if got := gjson.GetBytes(out, "input.0.role").String(); got != "system" {
		t.Fatalf("expected role system, got %s", got)
	}
	if got := gjson.GetBytes(out, "input.0.content.0.text").String(); got != "You are helpful" {
		t.Fatalf("expected system text, got %s", got)
	}
}

func TestConvertMapsGenerationParams(t *testing.T) {
	req := []byte(`{"messages":[{"role":"user","content":"hi"}],"max_tokens":100,"temperature":0.5,"top_p":0.9,"reasoning_effort":"high"}`)
	out := ConvertOpenAIRequestToGrokCLI("grok-cli/grok-4.3", req, false)

	if got := gjson.GetBytes(out, "max_output_tokens").Int(); got != 100 {
		t.Fatalf("expected max_output_tokens 100, got %d", got)
	}
	if got := gjson.GetBytes(out, "temperature").Float(); got != 0.5 {
		t.Fatalf("expected temperature 0.5, got %f", got)
	}
	if got := gjson.GetBytes(out, "top_p").Float(); got != 0.9 {
		t.Fatalf("expected top_p 0.9, got %f", got)
	}
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "high" {
		t.Fatalf("expected reasoning_effort high, got %s", got)
	}
}

func TestConvertFlattensFunctionTools(t *testing.T) {
	req := []byte(`{"messages":[{"role":"user","content":"weather"}],"tools":[{"type":"function","function":{"name":"get_weather","description":"weather","parameters":{"type":"object"},"strict":true}}]}`)
	out := ConvertOpenAIRequestToGrokCLI("grok-cli/grok-4.3", req, true)

	items := gjson.GetBytes(out, "tools").Array()
	if len(items) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(items))
	}
	if got := items[0].Get("type").String(); got != "function" {
		t.Fatalf("expected tool type function, got %s", got)
	}
	if got := items[0].Get("name").String(); got != "get_weather" {
		t.Fatalf("expected name get_weather, got %s", got)
	}
	if got := items[0].Get("description").String(); got != "weather" {
		t.Fatalf("expected description weather, got %s", got)
	}
	if !items[0].Get("parameters").Exists() {
		t.Fatalf("expected parameters field")
	}
	if !items[0].Get("strict").Bool() {
		t.Fatalf("expected strict true")
	}
	// Ensure the nested "function" wrapper is gone.
	if items[0].Get("function").Exists() {
		t.Fatalf("expected flattened tool, found nested function object")
	}
}

func TestConvertMultimodalParts(t *testing.T) {
	req := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image_url","image_url":{"url":"https://example.com/img.png"}},{"type":"file","file":{"file_data":"data:pdf;base64,xyz","filename":"doc.pdf"}}]}]}`)
	out := ConvertOpenAIRequestToGrokCLI("grok-cli/grok-4.3", req, true)

	parts := gjson.GetBytes(out, "input.0.content").Array()
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	if got := parts[0].Get("type").String(); got != "input_text" {
		t.Fatalf("expected input_text, got %s", got)
	}
	if got := parts[1].Get("type").String(); got != "input_image" {
		t.Fatalf("expected input_image, got %s", got)
	}
	if got := parts[2].Get("type").String(); got != "input_file" {
		t.Fatalf("expected input_file, got %s", got)
	}
}

func TestConvertToolMessages(t *testing.T) {
	req := []byte(`{"messages":[{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"LA\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"sunny"}]}`)
	out := ConvertOpenAIRequestToGrokCLI("grok-cli/grok-4.3", req, true)

	items := gjson.GetBytes(out, "input").Array()
	if len(items) != 2 {
		t.Fatalf("expected 2 input items, got %d", len(items))
	}
	if got := items[0].Get("type").String(); got != "function_call" {
		t.Fatalf("expected function_call, got %s", got)
	}
	if got := items[0].Get("call_id").String(); got != "call_1" {
		t.Fatalf("expected call_id call_1, got %s", got)
	}
	if got := items[1].Get("type").String(); got != "function_call_output" {
		t.Fatalf("expected function_call_output, got %s", got)
	}
	if got := items[1].Get("output").String(); got != "sunny" {
		t.Fatalf("expected output sunny, got %s", got)
	}
}
