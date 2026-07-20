package kiro

import (
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func TestClaudeToKiroIngressWired(t *testing.T) {
	body := []byte(`{
		"model": "claude-sonnet-4-6",
		"max_tokens": 4096,
		"messages": [
			{"role": "user", "content": "Registry ping"}
		]
	}`)
	out := registry.Request(string(types.FormatClaude), string(types.FormatKiro), "claude-sonnet-4-6", body, false)
	if !strings.Contains(string(out), "conversationState") {
		t.Fatalf("expected Kiro conversationState payload, got %s", out)
	}
}
