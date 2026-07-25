package antigravity

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/tidwall/gjson"
)

func TestAntigravityToClaudeResponseNonStream_Text(t *testing.T) {
	ctx := context.Background()
	providerResp := []byte(`{
		"response": {
			"responseId": "resp_ag_01",
			"modelVersion": "claude-sonnet-4-6",
			"candidates": [
				{
					"content": {
						"role": "model",
						"parts": [{"text": "Hello from Antigravity"}]
					},
					"finishReason": "STOP"
				}
			],
			"usageMetadata": {
				"promptTokenCount": 12,
				"candidatesTokenCount": 5,
				"totalTokenCount": 17
			}
		}
	}`)

	out := registry.ResponseNonStream(ctx, "claude", "antigravity", "claude-sonnet-4-6", nil, nil, providerResp, nil)
	if len(out) == 0 {
		t.Fatalf("expected non-empty Claude response, got %q", string(out))
	}

	root := gjson.ParseBytes(out)
	if root.Get("type").String() != "message" {
		t.Errorf("type = %q, want message", root.Get("type").String())
	}
	if root.Get("role").String() != "assistant" {
		t.Errorf("role = %q, want assistant", root.Get("role").String())
	}
	if root.Get("content.0.type").String() != "text" {
		t.Errorf("content[0].type = %q, want text", root.Get("content.0.type").String())
	}
	if root.Get("content.0.text").String() != "Hello from Antigravity" {
		t.Errorf("content[0].text = %q", root.Get("content.0.text").String())
	}
	if root.Get("usage.input_tokens").Int() != 12 {
		t.Errorf("input_tokens = %d", root.Get("usage.input_tokens").Int())
	}
	if root.Get("usage.output_tokens").Int() != 5 {
		t.Errorf("output_tokens = %d", root.Get("usage.output_tokens").Int())
	}
	if root.Get("stop_reason").String() != "end_turn" {
		t.Errorf("stop_reason = %q", root.Get("stop_reason").String())
	}
}

func TestAntigravityToClaudeResponseStream_Text(t *testing.T) {
	ctx := context.Background()
	var param any

	chunk := []byte("data: " + `{
		"response": {
			"responseId": "resp_ag_02",
			"modelVersion": "claude-sonnet-4-6",
			"candidates": [
				{
					"content": {
						"role": "model",
						"parts": [{"text": "Streamed text"}]
					},
					"finishReason": "STOP"
				}
			],
			"usageMetadata": {
				"promptTokenCount": 10,
				"candidatesTokenCount": 3,
				"totalTokenCount": 13
			}
		}
	}` + "\n\n")

	chunks := registry.Response(ctx, "claude", "antigravity", "claude-sonnet-4-6", nil, nil, chunk, &param)
	if len(chunks) == 0 {
		t.Fatalf("expected streamed Claude events, got none")
	}
	all := strings.Join(slicesFrom(chunks), "\n")
	for _, want := range []string{"event: message_start", "event: content_block_start", "event: content_block_delta", "event: message_delta", "event: message_stop"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in streamed output:\n%s", want, all)
		}
	}
	if !strings.Contains(all, "Streamed text") {
		t.Errorf("missing streamed text in output:\n%s", all)
	}
}

func TestClaudeToAntigravityRoundTrip(t *testing.T) {
	claudeReq := []byte(`{
		"model": "claude-sonnet-4-6",
		"max_tokens": 200,
		"messages": [{"role": "user", "content": "Ping Antigravity"}]
	}`)

	providerReq := registry.Request("claude", "antigravity", "claude-sonnet-4-6", claudeReq, false)
	if !gjson.ParseBytes(providerReq).Get("request").Exists() {
		t.Fatalf("expected Antigravity request envelope, got %s", string(providerReq))
	}

	providerResp := []byte(`{
		"response": {
			"responseId": "resp_roundtrip",
			"modelVersion": "claude-sonnet-4-6",
			"candidates": [
				{
					"content": {"role": "model", "parts": [{"text": "Pong"}]},
					"finishReason": "STOP"
				}
			],
			"usageMetadata": {"promptTokenCount": 4, "candidatesTokenCount": 2, "totalTokenCount": 6}
		}
	}`)

	claudeResp := registry.ResponseNonStream(context.Background(), "claude", "antigravity", "claude-sonnet-4-6", claudeReq, providerReq, providerResp, nil)
	root := gjson.ParseBytes(claudeResp)
	if root.Get("content.0.text").String() != "Pong" {
		t.Errorf("round-trip text = %q, want Pong", root.Get("content.0.text").String())
	}
	if root.Get("stop_reason").String() != "end_turn" {
		t.Errorf("round-trip stop_reason = %q", root.Get("stop_reason").String())
	}
}

func slicesFrom(chunks [][]byte) []string {
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, string(bytes.TrimSpace(c)))
	}
	return out
}
