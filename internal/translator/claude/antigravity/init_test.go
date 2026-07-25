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
	if !root.Get("request").Exists() {
		t.Fatalf("expected Antigravity request envelope; got %s", string(out))
	}

	// The tool_result should be translated into a functionResponse part inside
	// a user-role content turn, not remain inside the same user message.
	var sawFunctionResponse bool
	root.Get("request.contents").ForEach(func(_, m gjson.Result) bool {
		m.Get("parts").ForEach(func(_, p gjson.Result) bool {
			if p.Get("functionResponse").Exists() {
				sawFunctionResponse = true
			}
			return true
		})
		return true
	})
	if !sawFunctionResponse {
		t.Error("expected a functionResponse part for the tool_result")
	}
}
