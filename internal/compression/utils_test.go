package compression

import "testing"

func TestHasTools(t *testing.T) {
	if !HasTools([]byte(`{"tools":[{"type":"function"}]}`)) {
		t.Error("expected HasTools=true for non-empty tools")
	}
	if HasTools([]byte(`{"tools":[]}`)) {
		t.Error("expected HasTools=false for empty tools array")
	}
	if HasTools([]byte(`{"model":"gpt-4"}`)) {
		t.Error("expected HasTools=false when tools absent")
	}
}

func TestHasCacheControl(t *testing.T) {
	if !HasCacheControl([]byte(`{"messages":[{"role":"user","content":"hi","cache_control":{"type":"ephemeral"}}]}`)) {
		t.Error("expected HasCacheControl=true for message-level cache_control")
	}
	if !HasCacheControl([]byte(`{"system":[{"type":"text","text":"x","cache_control":{"type":"ephemeral"}}]}`)) {
		t.Error("expected HasCacheControl=true for system-level cache_control")
	}
	if HasCacheControl([]byte(`{"messages":[{"role":"user","content":"hi"}]}`)) {
		t.Error("expected HasCacheControl=false when cache_control absent")
	}
}
