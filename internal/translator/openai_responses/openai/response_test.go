package openai

import (
	"bytes"
	"context"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/tidwall/gjson"
)

func dataLine(s string) []byte {
	return []byte("data: " + s + "\n\n")
}

func TestConvertOpenAIChatToOpenAIResponsesNonStream_Text(t *testing.T) {
	resp := []byte(`{
		"id": "chatcmpl-test123",
		"object": "chat.completion",
		"created": 1735689600,
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": "Hello, world!"},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 3, "total_tokens": 13}
	}`)

	out := ConvertOpenAIChatToOpenAIResponsesNonStream(context.Background(), "", nil, nil, resp, nil)
	root := gjson.ParseBytes(out)
	if root.Get("object").String() != "response" {
		t.Fatalf("expected object=response, got %s", root.Get("object").String())
	}
	if root.Get("id").String() != "resp_test123" {
		t.Fatalf("unexpected response id: %s", root.Get("id").String())
	}
	if root.Get("output.0.type").String() != "message" {
		t.Fatalf("expected output item type message, got %s", root.Get("output.0.type").String())
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
	if root.Get("usage.total_tokens").Int() != 13 {
		t.Fatalf("unexpected total tokens")
	}
}

func TestConvertOpenAIChatToOpenAIResponsesNonStream_FunctionCall(t *testing.T) {
	resp := []byte(`{
		"id": "chatcmpl-fc",
		"object": "chat.completion",
		"created": 1735689600,
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "",
				"tool_calls": [{
					"id": "call_abc",
					"type": "function",
					"function": {"name": "get_weather", "arguments": "{\"city\":\"Paris\"}"}
				}]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`)

	out := ConvertOpenAIChatToOpenAIResponsesNonStream(context.Background(), "", nil, nil, resp, nil)
	root := gjson.ParseBytes(out)
	if root.Get("output.0.type").String() != "function_call" {
		t.Fatalf("expected function_call item, got %s", root.Get("output.0.type").String())
	}
	if root.Get("output.0.name").String() != "get_weather" {
		t.Fatalf("unexpected function name")
	}
	if root.Get("output.0.call_id").String() != "call_abc" {
		t.Fatalf("unexpected call_id")
	}
	args := gjson.Parse(root.Get("output.0.arguments").String())
	if args.Get("city").String() != "Paris" {
		t.Fatalf("unexpected arguments: %s", args.Raw)
	}
}

func TestConvertOpenAIChatToOpenAIResponsesNonStream_UsageDetails(t *testing.T) {
	resp := []byte(`{
		"id": "chatcmpl-usage",
		"object": "chat.completion",
		"created": 1735689600,
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": "Hi"},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 8,
			"completion_tokens": 2,
			"total_tokens": 10,
			"prompt_tokens_details": {"cached_tokens": 4},
			"completion_tokens_details": {"reasoning_tokens": 1}
		}
	}`)

	out := ConvertOpenAIChatToOpenAIResponsesNonStream(context.Background(), "", nil, nil, resp, nil)
	root := gjson.ParseBytes(out)
	if root.Get("usage.input_tokens_details.cached_tokens").Int() != 4 {
		t.Fatalf("expected cached tokens")
	}
	if root.Get("usage.output_tokens_details.reasoning_tokens").Int() != 1 {
		t.Fatalf("expected reasoning tokens")
	}
}

func TestConvertOpenAIChatToOpenAIResponsesStream_Text(t *testing.T) {
	chunks := [][]byte{
		dataLine(`{"id":"chatcmpl-stream","object":"chat.completion.chunk","created":1735689600,"model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"}}]}`),
		dataLine(`{"id":"chatcmpl-stream","object":"chat.completion.chunk","created":1735689600,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":", world!"},"finish_reason":"stop"}]}`),
	}

	var param any
	var allEvents [][]byte
	for _, chunk := range chunks {
		ev := ConvertOpenAIChatToOpenAIResponsesStream(context.Background(), "", nil, nil, chunk, &param)
		allEvents = append(allEvents, ev...)
	}

	var hasCreated, hasInProgress, hasAdded, hasPartAdded, hasDelta, hasTextDone, hasPartDone, hasItemDone, hasCompleted bool
	for _, ev := range allEvents {
		raw := bytes.TrimSpace(ev)
		if !bytes.HasPrefix(raw, []byte("data:")) {
			t.Fatalf("expected SSE data prefix")
		}
		data := gjson.ParseBytes(bytes.TrimSpace(raw[5:]))
		switch data.Get("type").String() {
		case "response.created":
			hasCreated = true
		case "response.in_progress":
			hasInProgress = true
		case "response.output_item.added":
			hasAdded = true
		case "response.content_part.added":
			hasPartAdded = true
		case "response.output_text.delta":
			hasDelta = true
		case "response.output_text.done":
			hasTextDone = true
		case "response.content_part.done":
			hasPartDone = true
		case "response.output_item.done":
			hasItemDone = true
		case "response.completed":
			hasCompleted = true
			if data.Get("response.status").String() != "completed" {
				t.Fatalf("expected completed status")
			}
		}
	}
	if !hasCreated || !hasInProgress || !hasAdded || !hasPartAdded || !hasDelta || !hasTextDone || !hasPartDone || !hasItemDone || !hasCompleted {
		t.Fatalf("missing expected events: created=%v in_progress=%v added=%v partAdded=%v delta=%v textDone=%v partDone=%v itemDone=%v completed=%v",
			hasCreated, hasInProgress, hasAdded, hasPartAdded, hasDelta, hasTextDone, hasPartDone, hasItemDone, hasCompleted)
	}
}

func TestConvertOpenAIChatToOpenAIResponsesStream_FunctionCall(t *testing.T) {
	chunks := [][]byte{
		dataLine(`{"id":"chatcmpl-fc","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_fc","function":{"name":"get_weather"}}]}}]}`),
		dataLine(`{"id":"chatcmpl-fc","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":\""}}]}}]}`),
		dataLine(`{"id":"chatcmpl-fc","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"Paris\"}"}}]},"finish_reason":"tool_calls"}]}`),
	}

	var param any
	var allEvents [][]byte
	for _, chunk := range chunks {
		ev := ConvertOpenAIChatToOpenAIResponsesStream(context.Background(), "", nil, nil, chunk, &param)
		allEvents = append(allEvents, ev...)
	}

	var hasAdded, hasArgsDelta, hasDone, hasCompleted bool
	var finalArgs string
	for _, ev := range allEvents {
		data := gjson.ParseBytes(bytes.TrimSpace(bytes.TrimSpace(ev)[5:]))
		switch data.Get("type").String() {
		case "response.output_item.added":
			hasAdded = true
		case "response.function_call_arguments.delta":
			hasArgsDelta = true
		case "response.output_item.done":
			hasDone = true
			finalArgs = data.Get("item.arguments").String()
		case "response.completed":
			hasCompleted = true
		}
	}
	if !hasAdded || !hasArgsDelta || !hasDone || !hasCompleted {
		t.Fatalf("missing expected function call events: added=%v argsDelta=%v done=%v completed=%v", hasAdded, hasArgsDelta, hasDone, hasCompleted)
	}
	args := gjson.Parse(finalArgs)
	if args.Get("city").String() != "Paris" {
		t.Fatalf("unexpected final arguments: %s", finalArgs)
	}
}

func TestConvertOpenAIChatToOpenAIResponsesStream_IgnoresEmptyAndDone(t *testing.T) {
	var param any
	if out := ConvertOpenAIChatToOpenAIResponsesStream(context.Background(), "", nil, nil, []byte("\n"), &param); out != nil {
		t.Fatalf("expected nil for empty chunk")
	}
	if out := ConvertOpenAIChatToOpenAIResponsesStream(context.Background(), "", nil, nil, []byte("data: [DONE]\n\n"), &param); out != nil {
		t.Fatalf("expected nil for [DONE]")
	}
}

func TestRegistryResponse_OpenAIToOpenAIResponses(t *testing.T) {
	if !registry.NeedConvert("openai", "openai-responses") {
		t.Fatalf("expected registry response transformer openai -> openai-responses")
	}
}
