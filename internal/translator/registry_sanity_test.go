package translator

import (
	"context"
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func TestAntigravityClaudePairIsWired(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","max_tokens":100,"messages":[{"role":"user","content":"ping"}]}`)
	raw := registry.Request(string(types.FormatClaude), string(types.FormatAntigravity), "claude-sonnet-4-6", body, false)
	if !strings.Contains(string(raw), "request") {
		t.Fatalf("claude->antigravity request transform not wired: %s", string(raw))
	}
	if !registry.NeedConvert(string(types.FormatAntigravity), string(types.FormatClaude)) {
		t.Fatal("missing response transformer (antigravity -> claude)")
	}

	ag := []byte(`{"response":{"responseId":"ag-1","modelVersion":"ag-model","candidates":[{"content":{"parts":[{"text":"hello"}]}}]}}`)
	var param any
	out := registry.ResponseNonStream(context.Background(), string(types.FormatAntigravity), string(types.FormatClaude), "ag-model", nil, nil, ag, &param)
	if !strings.Contains(string(out), `"type":"message"`) {
		t.Fatalf("expected Claude message response, got %s", string(out))
	}
}

func TestKiroClaudePairIsWired(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4","messages":[{"role":"user","content":"ping"}]}`)
	raw := registry.Request(string(types.FormatClaude), string(types.FormatKiro), "claude-sonnet-4", body, false)
	if !strings.Contains(string(raw), "conversationState") {
		t.Fatalf("claude->kiro request transform not wired: %s", string(raw))
	}
	if !registry.NeedConvert(string(types.FormatKiro), string(types.FormatClaude)) {
		t.Fatal("missing response transformer (kiro -> claude)")
	}

	openaiResp := []byte(`{"id":"chatcmpl-1","object":"chat.completion","model":"claude-sonnet-4","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`)
	var param any
	out := registry.ResponseNonStream(context.Background(), string(types.FormatKiro), string(types.FormatClaude), "claude-sonnet-4", nil, nil, openaiResp, &param)
	if !strings.Contains(string(out), `"type":"message"`) {
		t.Fatalf("expected Claude message response, got %s", string(out))
	}
}
