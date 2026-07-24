package executor

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestApplyClaudeCacheControl_InjectAllBreakpoints(t *testing.T) {
	body := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"max_tokens": 1024,
		"system": "You are a helpful assistant.",
		"tools": [
			{"name": "tool_a", "input_schema": {"type": "object"}},
			{"name": "tool_b", "input_schema": {"type": "object"}}
		],
		"messages": [
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi there"},
			{"role": "user", "content": "What can you do?"}
		]
	}`)

	out := applyClaudeCacheControl(body)

	if count := countCacheControls(out); count != 3 {
		t.Fatalf("expected 3 cache breakpoints, got %d", count)
	}

	if !gjson.GetBytes(out, "tools.1.cache_control").Exists() {
		t.Errorf("expected cache_control on last tool")
	}
	if !gjson.GetBytes(out, "system.0.cache_control").Exists() {
		t.Errorf("expected cache_control on system element")
	}
	if gjson.GetBytes(out, "system.0.type").String() != "text" {
		t.Errorf("expected system converted to array with type=text, got %s", gjson.GetBytes(out, "system.0.type").String())
	}
	if !gjson.GetBytes(out, "messages.0.content.0.cache_control").Exists() {
		t.Errorf("expected cache_control on second-to-last user turn")
	}
}

func TestApplyClaudeCacheControl_DoesNotOverrideExistingClientCache(t *testing.T) {
	body := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"system": [{"type": "text", "text": "sys"}],
		"tools": [
			{"name": "tool_a", "input_schema": {"type": "object"}, "cache_control": {"type": "ephemeral"}},
			{"name": "tool_b", "input_schema": {"type": "object"}}
		],
		"messages": [
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi"},
			{"role": "user", "content": "What can you do?"}
		]
	}`)

	out := applyClaudeCacheControl(body)

	// Existing tool cache should be preserved, and no additional tool cache injected.
	toolWithCC := 0
	gjson.GetBytes(out, "tools").ForEach(func(_, item gjson.Result) bool {
		if item.Get("cache_control").Exists() {
			toolWithCC++
		}
		return true
	})
	if toolWithCC != 1 {
		t.Errorf("expected 1 tool with cache_control, got %d", toolWithCC)
	}

	// System should still receive cache_control because its own section has none.
	if !gjson.GetBytes(out, "system.0.cache_control").Exists() {
		t.Errorf("expected system cache_control to be injected when no existing system cache")
	}
}

func TestApplyClaudeCacheControl_SkipsMessagesWhenAlreadyCached(t *testing.T) {
	body := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "Hello", "cache_control": {"type": "ephemeral"}}]},
			{"role": "assistant", "content": "Hi"},
			{"role": "user", "content": "What can you do?"}
		]
	}`)

	out := applyClaudeCacheControl(body)

	if count := countCacheControls(out); count != 1 {
		t.Fatalf("expected 1 cache breakpoint (client-provided), got %d", count)
	}

	if gjson.GetBytes(out, "messages.2.content.0.cache_control").Exists() {
		t.Errorf("should not inject cache_control into messages when client already set one")
	}
}

func TestApplyClaudeCacheControl_SingleUserTurn_NoMessageCache(t *testing.T) {
	body := []byte(`{
		"model": "claude-3-5-sonnet-20241022",
		"system": "You are helpful.",
		"messages": [
			{"role": "user", "content": "Hello"}
		]
	}`)

	out := applyClaudeCacheControl(body)

	if count := countCacheControls(out); count != 1 {
		t.Fatalf("expected 1 cache breakpoint (system only), got %d", count)
	}
}

func TestCountCacheControls(t *testing.T) {
	payload := []byte(`{
		"system": [
			{"type": "text", "text": "a", "cache_control": {"type": "ephemeral"}},
			{"type": "text", "text": "b", "cache_control": {"type": "ephemeral"}}
		],
		"tools": [
			{"name": "t1", "cache_control": {"type": "ephemeral"}}
		],
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "hi", "cache_control": {"type": "ephemeral"}}]}
		]
	}`)

	if got := countCacheControls(payload); got != 4 {
		t.Fatalf("expected 4 cache controls, got %d", got)
	}
}

func TestEnforceCacheControlLimit_RemovesExcess(t *testing.T) {
	payload := []byte(`{
		"system": [
			{"type": "text", "text": "a", "cache_control": {"type": "ephemeral"}},
			{"type": "text", "text": "b", "cache_control": {"type": "ephemeral"}}
		],
		"tools": [
			{"name": "t1", "cache_control": {"type": "ephemeral"}},
			{"name": "t2", "cache_control": {"type": "ephemeral"}}
		],
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "hi", "cache_control": {"type": "ephemeral"}}]},
			{"role": "assistant", "content": "hello"},
			{"role": "user", "content": [{"type": "text", "text": "bye", "cache_control": {"type": "ephemeral"}}]}
		]
	}`)

	out := enforceCacheControlLimit(payload, 4)
	if got := countCacheControls(out); got != 4 {
		t.Fatalf("expected 4 cache controls after limit, got %d", got)
	}

	// First system should be stripped before last system.
	if gjson.GetBytes(out, "system.0.cache_control").Exists() {
		t.Errorf("expected first system cache_control to be stripped")
	}
	if !gjson.GetBytes(out, "system.1.cache_control").Exists() {
		t.Errorf("expected last system cache_control to be preserved")
	}
}

func TestNormalizeCacheControlTTL_DowngradesInvalid1hAfter5m(t *testing.T) {
	payload := []byte(`{
		"system": [
			{"type": "text", "text": "a", "cache_control": {"type": "ephemeral", "ttl": "5m"}},
			{"type": "text", "text": "b", "cache_control": {"type": "ephemeral", "ttl": "1h"}}
		]
	}`)

	out := normalizeCacheControlTTL(payload)

	// First 5m stays.
	if gjson.GetBytes(out, "system.0.cache_control.ttl").String() != "5m" {
		t.Errorf("expected first system ttl to remain 5m")
	}
	// Second 1h should be downgraded to default (ttl removed).
	if gjson.GetBytes(out, "system.1.cache_control.ttl").Exists() {
		t.Errorf("expected second system 1h ttl to be removed")
	}
	if !gjson.GetBytes(out, "system.1.cache_control").Exists() {
		t.Errorf("expected second system cache_control to remain")
	}
}

func TestNormalizeCacheControlTTL_KeepsValid1hBefore5m(t *testing.T) {
	payload := []byte(`{
		"system": [
			{"type": "text", "text": "a", "cache_control": {"type": "ephemeral", "ttl": "1h"}},
			{"type": "text", "text": "b", "cache_control": {"type": "ephemeral", "ttl": "5m"}}
		]
	}`)

	out := normalizeCacheControlTTL(payload)

	if gjson.GetBytes(out, "system.0.cache_control.ttl").String() != "1h" {
		t.Errorf("expected first system ttl to remain 1h, got %s", gjson.GetBytes(out, "system.0.cache_control.ttl").String())
	}
	if gjson.GetBytes(out, "system.1.cache_control.ttl").String() != "5m" {
		t.Errorf("expected second system ttl to remain 5m")
	}
}

func TestNormalizeCacheControlTTL_ToolsThenSystem(t *testing.T) {
	payload := []byte(`{
		"tools": [
			{"name": "t1", "cache_control": {"type": "ephemeral"}}
		],
		"system": [
			{"type": "text", "text": "a", "cache_control": {"type": "ephemeral", "ttl": "1h"}}
		]
	}`)

	out := normalizeCacheControlTTL(payload)

	// Tools has no explicit ttl => default 5m. After that, system 1h should be downgraded.
	if gjson.GetBytes(out, "system.0.cache_control.ttl").Exists() {
		t.Errorf("expected system 1h ttl to be stripped after default-ttl tool")
	}
}

func TestApplyClaudeCacheControl_InvalidBody(t *testing.T) {
	out := applyClaudeCacheControl([]byte("not json"))
	if string(out) != "not json" {
		t.Errorf("expected invalid body to be returned unchanged")
	}
}

func TestInjectToolsCacheControl(t *testing.T) {
	payload := []byte(`{"tools": [{"name": "a"}, {"name": "b"}]}`)
	out := injectToolsCacheControl(payload)

	if !gjson.GetBytes(out, "tools.1.cache_control").Exists() {
		t.Errorf("expected cache_control on last tool")
	}
	if gjson.GetBytes(out, "tools.0.cache_control").Exists() {
		t.Errorf("did not expect cache_control on first tool")
	}
}

func TestInjectSystemCacheControl_String(t *testing.T) {
	payload := []byte(`{"system": "You are helpful."}`)
	out := injectSystemCacheControl(payload)

	if gjson.GetBytes(out, "system").Type != gjson.JSON {
		t.Fatalf("expected system to become an array")
	}
	if !gjson.GetBytes(out, "system.0.cache_control").Exists() {
		t.Errorf("expected cache_control on converted system element")
	}
}

func TestInjectMessagesCacheControl_StringContent(t *testing.T) {
	payload := []byte(`{
		"messages": [
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi"},
			{"role": "user", "content": "What can you do?"}
		]
	}`)
	out := injectMessagesCacheControl(payload)

	if !gjson.GetBytes(out, "messages.0.content").IsArray() {
		t.Fatalf("expected string content converted to array")
	}
	if !gjson.GetBytes(out, "messages.0.content.0.cache_control").Exists() {
		t.Errorf("expected cache_control on converted content")
	}
}
