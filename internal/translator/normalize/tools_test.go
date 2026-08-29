package normalize

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestEnsureToolCallIDs(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"type":"function","function":{"name":"read"}}]}]}`)
	out := EnsureToolCallIDs(body)
	if got := gjson.GetBytes(out, "messages.0.tool_calls.0.id").String(); got == "" {
		t.Fatal("expected generated tool call id")
	}
}

func TestFixMissingToolResponses(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","tool_calls":[			{"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}}]},{"role":"user","content":"continue"}]}`)
	out := FixMissingToolResponses(EnsureToolCallIDs(body))
	if got := gjson.GetBytes(out, "messages.1.tool_call_id").String(); got != "call_1" {
		t.Fatalf("tool_call_id = %q", got)
	}
}

func TestEnsureToolCallIDsSanitizesAndNormalizes(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"bad.id/1","function":{"arguments":{"x":1}}}]}]}`)
	out := EnsureToolCallIDs(body)
	if got := gjson.GetBytes(out, "messages.0.tool_calls.0.id").String(); got != "badid1" {
		t.Fatalf("sanitized id = %q", got)
	}
	if got := gjson.GetBytes(out, "messages.0.tool_calls.0.type").String(); got != "function" {
		t.Fatalf("type = %q", got)
	}
	if got := gjson.GetBytes(out, "messages.0.tool_calls.0.function.arguments").String(); got != `{"x":1}` {
		t.Fatalf("arguments = %q", got)
	}
}

func TestEnsureResponsesCallIDs(t *testing.T) {
	body := []byte(`{"input":[{"type":"function_call","id":"bad.id"}]}`)
	out := EnsureToolCallIDs(body)
	if got := gjson.GetBytes(out, "input.0.call_id").String(); got != "badid" {
		t.Fatalf("call_id = %q", got)
	}
}

func TestFixMissingToolResponsesPreservesExisting(t *testing.T) {
	body := []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"call_1"}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]}`)
	out := FixMissingToolResponses(body)
	if got := gjson.GetBytes(out, "messages.#").Int(); got != 2 {
		t.Fatalf("message count = %d", got)
	}
}
