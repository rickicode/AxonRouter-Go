package antigravity

import (
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
	"github.com/tidwall/gjson"
)

func TestClaudeToAntigravityIngressWired(t *testing.T) {
	body := []byte(`{
		"model": "ag/claude-sonnet-4-6",
		"max_tokens": 200,
		"messages": [
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "toolu_1", "content": "42"},
				{"type": "text", "text": "result?"}
			]}
		]
	}`)
	out := registry.Request(string(types.FormatClaude), string(types.FormatAntigravity), "claude-sonnet-4-6", body, false)
	root := gjson.ParseBytes(out)
	if root.Get("model").String() != "claude-sonnet-4-6" {
		t.Fatalf("model = %q, want claude-sonnet-4-6", root.Get("model").String())
	}
	var sawToolRole, sawToolResultInUser bool
	root.Get("messages").ForEach(func(_, m gjson.Result) bool {
		if m.Get("role").String() == "tool" {
			sawToolRole = true
		}
		if m.Get("role").String() == "user" {
			m.Get("content").ForEach(func(_, p gjson.Result) bool {
				if p.Get("type").String() == "tool_result" {
					sawToolResultInUser = true
				}
				return true
			})
		}
		return true
	})
	if !sawToolRole {
		t.Error("expected a role:tool message via the claude→antigravity ingress")
	}
	if sawToolResultInUser {
		t.Error("tool_result must not remain inside the user message")
	}
}
