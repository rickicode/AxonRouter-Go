package openai

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConvertOpenAINonStreamToResponses(t *testing.T) {
	input := []byte(`{
		"id": "chatcmpl-abc",
		"object": "chat.completion",
		"created": 1234567890,
		"model": "gpt-4o",
		"choices": [
			{
				"index": 0,
				"message": {"role": "assistant", "content": "Hello!"},
				"finish_reason": "stop"
			}
		],
		"usage": {"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12}
	}`)

	out := convertOpenAINonStreamToResponses(context.Background(), "", nil, nil, input, nil)

	if got := gjson.GetBytes(out, "object").String(); got != "response" {
		t.Errorf("object=%q, want response", got)
	}
	if got := gjson.GetBytes(out, "output.0.type").String(); got != "message" {
		t.Errorf("output[0].type=%q, want message", got)
	}
	if got := gjson.GetBytes(out, "output.0.content.0.text").String(); got != "Hello!" {
		t.Errorf("text=%q, want Hello!", got)
	}
	if got := gjson.GetBytes(out, "usage.total_tokens").Int(); got != 12 {
		t.Errorf("total_tokens=%d, want 12", got)
	}
}

func TestConvertOpenAIStreamToResponses(t *testing.T) {
	chunks := [][]byte{
		[]byte(`data: {"id":"chatcmpl-stream","created":1234567890,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}` + "\n\n"),
		[]byte(`data: {"id":"chatcmpl-stream","created":1234567890,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}` + "\n\n"),
	}

	var param any
	var events [][]byte
	for _, ch := range chunks {
		ev := convertOpenAIStreamToResponses(context.Background(), "", nil, nil, ch, &param)
		events = append(events, ev...)
	}

	body := bytes.Join(events, []byte(""))
	if !strings.Contains(string(body), `"type":"response.created"`) {
		t.Errorf("expected response.created event, got %s", body)
	}
	if !strings.Contains(string(body), `"type":"response.output_text.delta"`) {
		t.Errorf("expected output_text.delta event, got %s", body)
	}
	if !strings.Contains(string(body), `"type":"response.completed"`) {
		t.Errorf("expected response.completed event, got %s", body)
	}
	if !strings.Contains(string(body), `"text":"Hello!"`) {
		t.Errorf("expected aggregated text Hello!, got %s", body)
	}
	if !strings.Contains(string(body), `"total_tokens":3`) {
		t.Errorf("expected usage total_tokens, got %s", body)
	}
}
