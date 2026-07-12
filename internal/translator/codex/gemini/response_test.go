package gemini

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertGeminiResponseToCodexNonStream_TextParts(t *testing.T) {
	resp := []byte(`{
		"id": "resp_1",
		"model": "gemini-2.0-flash",
		"candidates": [{"content": {"parts": [{"text": "Hello "}, {"text": "world!"}]}}],
		"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 4, "totalTokenCount": 9}
	}`)
	out := convertGeminiResponseToCodexNonStream(context.Background(), "", nil, nil, resp, nil)
	root := gjson.ParseBytes(out)
	if root.Get("output.0.type").String() != "message" {
		t.Fatalf("expected message output item")
	}
	if got := root.Get("output.0.content.0.text").String(); got != "Hello world!" {
		t.Fatalf("unexpected output text: %s", got)
	}
}

func TestConvertGeminiResponseToCodexNonStream_FunctionCallParts(t *testing.T) {
	resp := []byte(`{
		"id": "resp_fc",
		"model": "gemini-2.0-flash",
		"candidates": [{"content": {"parts": [{"functionCall": {"name": "get_weather", "args": {"city": "Paris"}}}]}}]
	}`)
	out := convertGeminiResponseToCodexNonStream(context.Background(), "", nil, nil, resp, nil)
	root := gjson.ParseBytes(out)
	if root.Get("output.0.type").String() != "function_call" {
		t.Fatalf("expected function_call output item: %s", root.Get("output.0.type").String())
	}
	if got := root.Get("output.0.name").String(); got != "get_weather" {
		t.Fatalf("unexpected name: %s", got)
	}
	if got := root.Get("output.0.arguments").String(); !gjson.Valid(got) {
		t.Fatalf("arguments not valid JSON: %s", got)
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(root.Get("output.0.arguments").String()), &m); err != nil {
		t.Fatalf("arguments not unmarshalable: %v", err)
	}
	if m["city"] != "Paris" {
		t.Fatalf("unexpected arguments object: %v", m)
	}
}

func TestConvertGeminiResponseToCodexNonStream_TextThenFunctionCall(t *testing.T) {
	resp := []byte(`{
		"id": "resp_mix",
		"model": "gemini-2.0-flash",
		"candidates": [{"content": {"parts": [
			{"text": "Sure, "},
			{"functionCall": {"name": "get_weather", "args": {"city": "Paris"}}}
		]}}]
	}`)
	out := convertGeminiResponseToCodexNonStream(context.Background(), "", nil, nil, resp, nil)
	root := gjson.ParseBytes(out)
	if root.Get("output.0.type").String() != "message" {
		t.Fatalf("expected first item message, got %s", root.Get("output.0.type").String())
	}
	if root.Get("output.1.type").String() != "function_call" {
		t.Fatalf("expected second item function_call, got %s", root.Get("output.1.type").String())
	}
}

func TestConvertGeminiResponseToCodexNonStream_UsageMetadata(t *testing.T) {
	resp := []byte(`{
		"candidates": [{}],
		"usageMetadata": {
			"promptTokenCount": 10,
			"candidatesTokenCount": 5,
			"totalTokenCount": 15,
			"cachedContentTokenCount": 2,
			"thoughtsTokenCount": 1
		}
	}`)
	out := convertGeminiResponseToCodexNonStream(context.Background(), "", nil, nil, resp, nil)
	root := gjson.ParseBytes(out)
	if root.Get("usage.input_tokens").Int() != 10 {
		t.Fatalf("unexpected input_tokens: %d", root.Get("usage.input_tokens").Int())
	}
	if root.Get("usage.output_tokens").Int() != 5 {
		t.Fatalf("unexpected output_tokens: %d", root.Get("usage.output_tokens").Int())
	}
	if root.Get("usage.cached_tokens").Int() != 2 {
		t.Fatalf("unexpected cached_tokens: %d", root.Get("usage.cached_tokens").Int())
	}
	if root.Get("usage.reasoning_tokens").Int() != 1 {
		t.Fatalf("unexpected reasoning_tokens: %d", root.Get("usage.reasoning_tokens").Int())
	}
}

func TestConvertGeminiResponseToCodexStream_TextDelta(t *testing.T) {
	resp := []byte(`{"candidates": [{"content": {"parts": [{"text": "Hi"}]}}]}`)
	var state any
	chunks := convertGeminiResponseToCodexStream(context.Background(), "", nil, nil, resp, &state)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	root := gjson.ParseBytes(chunks[0])
	if root.Get("type").String() != "response.output_text.delta" {
		t.Fatalf("unexpected event type: %s", root.Get("type").String())
	}
	if root.Get("delta").String() != "Hi" {
		t.Fatalf("unexpected delta: %s", root.Get("delta").String())
	}
}

func TestConvertGeminiResponseToCodexStream_FunctionCall(t *testing.T) {
	resp := []byte(`{"candidates": [{"content": {"parts": [{"functionCall": {"name": "get_weather", "args": {"city": "Paris"}}}]}}]}`)
	var state any
	chunks := convertGeminiResponseToCodexStream(context.Background(), "", nil, nil, resp, &state)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	root := gjson.ParseBytes(chunks[0])
	if root.Get("type").String() != "response.output_item.done" {
		t.Fatalf("unexpected event type: %s", root.Get("type").String())
	}
	if root.Get("item.type").String() != "function_call" {
		t.Fatalf("unexpected item type: %s", root.Get("item.type").String())
	}
	if root.Get("item.name").String() != "get_weather" {
		t.Fatalf("unexpected item name: %s", root.Get("item.name").String())
	}
}

func TestConvertGeminiResponseToCodexStream_CompletedEvent(t *testing.T) {
	resp := []byte(`{"candidates": [{"content": {"parts": []}, "finishReason": "STOP"}]}`)
	var state any
	chunks := convertGeminiResponseToCodexStream(context.Background(), "", nil, nil, resp, &state)
	found := false
	for _, c := range chunks {
		if gjson.ParseBytes(c).Get("type").String() == "response.completed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected response.completed event, got: %v", chunks)
	}
}
