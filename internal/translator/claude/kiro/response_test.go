package kiro

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/tidwall/gjson"
)

func TestKiroToClaudeResponseNonStream_Text(t *testing.T) {
	ctx := context.Background()
	providerResp := []byte(`{
		"id": "chatcmpl_kiro_01",
		"object": "chat.completion",
		"model": "claude-sonnet-4-6",
		"choices": [
			{
				"index": 0,
				"message": {"role": "assistant", "content": "Hello from Kiro"},
				"finish_reason": "stop"
			}
		],
		"usage": {"prompt_tokens": 8, "completion_tokens": 4}
	}`)

	out := registry.ResponseNonStream(ctx, "claude", "kiro", "claude-sonnet-4-6", nil, nil, providerResp, nil)
	if len(out) == 0 {
		t.Fatalf("expected non-empty Claude response")
	}

	root := gjson.ParseBytes(out)
	if root.Get("type").String() != "message" {
		t.Errorf("type = %q, want message", root.Get("type").String())
	}
	if root.Get("content.0.type").String() != "text" {
		t.Errorf("content[0].type = %q, want text", root.Get("content.0.type").String())
	}
	if root.Get("content.0.text").String() != "Hello from Kiro" {
		t.Errorf("content[0].text = %q", root.Get("content.0.text").String())
	}
	if root.Get("stop_reason").String() != "end_turn" {
		t.Errorf("stop_reason = %q", root.Get("stop_reason").String())
	}
}

func TestKiroToClaudeResponseNonStream_AssistantResponseEventFallback(t *testing.T) {
	ctx := context.Background()
	providerResp := []byte(`{"assistantResponseEvent": {"content": "Fallback Kiro text"}}`)

	out := registry.ResponseNonStream(ctx, "claude", "kiro", "claude-sonnet-4-6", nil, nil, providerResp, nil)
	if len(out) == 0 {
		t.Fatalf("expected non-empty Claude response")
	}

	root := gjson.ParseBytes(out)
	if root.Get("content.0.text").String() != "Fallback Kiro text" {
		t.Errorf("content[0].text = %q", root.Get("content.0.text").String())
	}
}

func TestKiroToClaudeResponseStream_Text(t *testing.T) {
	ctx := context.Background()
	var param any

	chunks := []string{
		`{"id":"chatcmpl_kiro_stream","object":"chat.completion.chunk","model":"claude-sonnet-4-6","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi "},"finish_reason":null}]}`,
		`{"id":"chatcmpl_kiro_stream","object":"chat.completion.chunk","model":"claude-sonnet-4-6","choices":[{"index":0,"delta":{"content":"Kiro"},"finish_reason":null}]}`,
		`{"id":"chatcmpl_kiro_stream","object":"chat.completion.chunk","model":"claude-sonnet-4-6","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	}

	var all strings.Builder
	for _, c := range chunks {
		out := registry.Response(ctx, "claude", "kiro", "claude-sonnet-4-6", nil, nil, []byte("data: "+c+"\n\n"), &param)
		for _, b := range out {
			all.Write(bytes.TrimSpace(b))
			all.WriteByte('\n')
		}
	}

	// Upstream [DONE] triggers the terminal Claude events.
	done := []byte("data: [DONE]\n\n")
	out := registry.Response(ctx, "claude", "kiro", "claude-sonnet-4-6", nil, nil, done, &param)
	for _, b := range out {
		all.Write(bytes.TrimSpace(b))
		all.WriteByte('\n')
	}

	for _, want := range []string{"message_start", "text_delta", "message_stop"} {
		if !strings.Contains(all.String(), `"type":"`+want+`"`) && !strings.Contains(all.String(), want) {
			t.Errorf("missing %q in streamed output:\n%s", want, all.String())
		}
	}
	if !strings.Contains(all.String(), `"text":"Hi "`) || !strings.Contains(all.String(), `"text":"Kiro"`) {
		t.Errorf("missing streamed text deltas in output:\n%s", all.String())
	}
}

func TestClaudeToKiroRoundTrip(t *testing.T) {
	ctx := context.Background()
	claudeReq := []byte(`{
		"model": "claude-sonnet-4-6",
		"max_tokens": 4096,
		"messages": [{"role": "user", "content": "Ping Kiro"}]
	}`)

	providerReq := registry.Request("claude", "kiro", "claude-sonnet-4-6", claudeReq, false)
	if !gjson.ParseBytes(providerReq).Get("conversationState").Exists() {
		t.Fatalf("expected Kiro conversationState payload, got %s", string(providerReq))
	}

	providerResp := []byte(`{
		"id": "chatcmpl_roundtrip",
		"object": "chat.completion",
		"model": "claude-sonnet-4-6",
		"choices": [{"index":0,"message":{"role":"assistant","content":"Pong"},"finish_reason":"stop"}],
		"usage": {"prompt_tokens":2,"completion_tokens":1}
	}`)

	claudeResp := registry.ResponseNonStream(ctx, "claude", "kiro", "claude-sonnet-4-6", claudeReq, providerReq, providerResp, nil)
	root := gjson.ParseBytes(claudeResp)
	if root.Get("content.0.text").String() != "Pong" {
		t.Errorf("round-trip text = %q, want Pong", root.Get("content.0.text").String())
	}
	if root.Get("stop_reason").String() != "end_turn" {
		t.Errorf("round-trip stop_reason = %q", root.Get("stop_reason").String())
	}
}
