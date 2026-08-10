package gemini

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func dataLine(s string) []byte {
	return []byte("data: " + s + "\n\n")
}

func TestConvertGeminiResponseToOpenAIResponsesNonStream_Text(t *testing.T) {
	resp := []byte(`{
		"modelVersion": "gemini-2.5-pro",
		"createTimeMillis": 1735689600000,
		"candidates": [{
			"content": {"role": "model", "parts": [{"text": "Hello, world!"}]},
			"finishReason": "STOP"
		}],
		"usageMetadata": {
			"promptTokenCount": 10,
			"candidatesTokenCount": 3,
			"totalTokenCount": 13
		}
	}`)

	out := convertGeminiResponseToOpenAIResponsesNonStream(context.Background(), "", nil, nil, resp, nil)
	root := gjson.ParseBytes(out)
	if root.Get("object").String() != "response" {
		t.Fatalf("expected object=response, got %s", root.Get("object").String())
	}
	if got := root.Get("output.0.type").String(); got != "message" {
		t.Fatalf("expected output item type message, got %s", got)
	}
	if got := root.Get("output.0.content.0.text").String(); got != "Hello, world!" {
		t.Fatalf("unexpected text: %s", got)
	}
	if root.Get("usage.input_tokens").Int() != 10 {
		t.Fatalf("unexpected prompt tokens")
	}
	if root.Get("usage.output_tokens").Int() != 3 {
		t.Fatalf("unexpected completion tokens")
	}
}

func TestConvertGeminiResponseToOpenAIResponsesNonStream_Reasoning(t *testing.T) {
	resp := []byte(`{
		"modelVersion": "gemini-2.5-pro",
		"createTimeMillis": 1735689600000,
		"candidates": [{
			"content": {"role": "model", "parts": [
				{"text": "I need to think", "thought": true},
				{"text": "Done"}
			]},
			"finishReason": "STOP"
		}],
		"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 5, "totalTokenCount": 10, "thoughtsTokenCount": 2}
	}`)

	out := convertGeminiResponseToOpenAIResponsesNonStream(context.Background(), "", nil, nil, resp, nil)
	root := gjson.ParseBytes(out)
	if root.Get("output.0.type").String() != "reasoning" {
		t.Fatalf("expected reasoning item first, got %s", root.Get("output.0.type").String())
	}
	if root.Get("output.1.type").String() != "message" {
		t.Fatalf("expected message item second")
	}
	if root.Get("usage.output_tokens_details.reasoning_tokens").Int() != 2 {
		t.Fatalf("expected reasoning tokens in usage")
	}
}

func TestConvertGeminiResponseToOpenAIResponsesNonStream_FunctionCall(t *testing.T) {
	resp := []byte(`{
		"modelVersion": "gemini-2.5-pro",
		"createTimeMillis": 1735689600000,
		"candidates": [{
			"content": {"role": "model", "parts": [{"functionCall": {"name": "get_weather", "args": {"city": "Paris"}}}]},
			"finishReason": "STOP"
		}],
		"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 5, "totalTokenCount": 15}
	}`)

	out := convertGeminiResponseToOpenAIResponsesNonStream(context.Background(), "", nil, nil, resp, nil)
	root := gjson.ParseBytes(out)
	if root.Get("output.0.type").String() != "function_call" {
		t.Fatalf("expected function_call item, got %s", root.Get("output.0.type").String())
	}
	if root.Get("output.0.name").String() != "get_weather" {
		t.Fatalf("unexpected function name")
	}
	args := gjson.Parse(root.Get("output.0.arguments").String())
	if args.Get("city").String() != "Paris" {
		t.Fatalf("unexpected arguments: %s", args.Raw)
	}
}

func TestConvertGeminiResponseToOpenAIResponsesStream_Text(t *testing.T) {
	chunk := dataLine(`{
		"modelVersion": "gemini-2.5-pro",
		"createTimeMillis": 1735689600000,
		"candidates": [{
			"content": {"role": "model", "parts": [{"text": "Hello"}]},
			"finishReason": "STOP"
		}],
		"usageMetadata": {"promptTokenCount": 1, "candidatesTokenCount": 1, "totalTokenCount": 2}
	}`)

	var param any
	events := convertGeminiResponseToOpenAIResponsesStream(context.Background(), "", nil, nil, chunk, &param)
	if len(events) == 0 {
		t.Fatalf("expected events")
	}

	var hasAdded, hasDelta, hasDone, hasCompleted bool
	for _, ev := range events {
		raw := bytes.TrimSpace(ev)
		if !bytes.HasPrefix(raw, []byte("data:")) {
			t.Fatalf("expected SSE data prefix")
		}
		data := gjson.ParseBytes(bytes.TrimSpace(raw[5:]))
		switch data.Get("type").String() {
		case "response.output_item.added":
			hasAdded = true
		case "response.output_text.delta":
			hasDelta = true
		case "response.output_item.done":
			hasDone = true
		case "response.completed":
			hasCompleted = true
			if data.Get("response.output.0.type").String() != "message" {
				t.Fatalf("expected message in completed output")
			}
		}
	}
	if !hasAdded || !hasDelta || !hasDone || !hasCompleted {
		t.Fatalf("missing expected events: added=%v delta=%v done=%v completed=%v", hasAdded, hasDelta, hasDone, hasCompleted)
	}
}

func TestConvertGeminiResponseToOpenAIResponsesStream_FunctionCall(t *testing.T) {
	chunk := dataLine(`{
		"modelVersion": "gemini-2.5-pro",
		"createTimeMillis": 1735689600000,
		"candidates": [{
			"content": {"role": "model", "parts": [{"functionCall": {"name": "get_weather", "args": {"city": "Paris"}}}]},
			"finishReason": "STOP"
		}],
		"usageMetadata": {"promptTokenCount": 1, "candidatesTokenCount": 1, "totalTokenCount": 2}
	}`)

	var param any
	events := convertGeminiResponseToOpenAIResponsesStream(context.Background(), "", nil, nil, chunk, &param)

	var hasAdded, hasArgsDelta, hasDone bool
	for _, ev := range events {
		data := gjson.ParseBytes(bytes.TrimSpace(bytes.TrimSpace(ev)[5:]))
		switch data.Get("type").String() {
		case "response.output_item.added":
			hasAdded = true
		case "response.function_call_arguments.delta":
			hasArgsDelta = true
		case "response.output_item.done":
			hasDone = true
		}
	}
	if !hasAdded || !hasArgsDelta || !hasDone {
		t.Fatalf("missing expected function call events")
	}
}

func TestConvertGeminiResponseToOpenAIResponsesStream_Reasoning(t *testing.T) {
	chunk := dataLine(`{
		"modelVersion": "gemini-2.5-pro",
		"createTimeMillis": 1735689600000,
		"candidates": [{
			"content": {"role": "model", "parts": [{"text": "Think", "thought": true}]},
			"finishReason": "STOP"
		}]
	}`)

	var param any
	events := convertGeminiResponseToOpenAIResponsesStream(context.Background(), "", nil, nil, chunk, &param)

	var hasReasoning bool
	var completedOutput gjson.Result
	for _, ev := range events {
		data := gjson.ParseBytes(bytes.TrimSpace(bytes.TrimSpace(ev)[5:]))
		if data.Get("type").String() == "response.output_item.added" {
			if data.Get("item.type").String() == "reasoning" {
				hasReasoning = true
			}
		}
		if data.Get("type").String() == "response.completed" {
			completedOutput = data.Get("response.output")
		}
	}
	if !hasReasoning {
		t.Fatalf("expected reasoning output_item.added event")
	}
	if !completedOutput.Exists() || completedOutput.Array()[0].Get("type").String() != "reasoning" {
		t.Fatalf("expected reasoning in completed output")
	}
}

func TestConvertGeminiResponseToOpenAIResponsesStream_IgnoresEmptyAndDone(t *testing.T) {
	var param any
	if out := convertGeminiResponseToOpenAIResponsesStream(context.Background(), "", nil, nil, []byte("\n"), &param); out != nil {
		t.Fatalf("expected nil for empty chunk")
	}
	if out := convertGeminiResponseToOpenAIResponsesStream(context.Background(), "", nil, nil, []byte("data: [DONE]\n\n"), &param); out != nil {
		t.Fatalf("expected nil for [DONE]")
	}
}

func TestConvertGeminiResponseToOpenAIResponsesStream_SSEPrefix(t *testing.T) {
	chunk := dataLine(`{"modelVersion":"gemini-test","createTimeMillis":1735689600000,"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}]}`)
	if !strings.HasPrefix(string(chunk), "data: ") {
		t.Fatalf("test helper should produce data: prefix")
	}
}
