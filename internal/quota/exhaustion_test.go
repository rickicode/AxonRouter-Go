package quota

import (
	"testing"
	"time"
)

func TestExhaustion_CompositeKey_Isolation(t *testing.T) {
	ec := NewExhaustionCache()

	// Mark a scoped model/family exhausted.
	key := ExhaustKey("conn-1", "family:gemini")
	ec.MarkExhausted(key, 5*time.Minute)

	// Same connection, different scope, must NOT be exhausted.
	if ec.IsExhausted("conn-1") {
		t.Error("connection-wide key should not be exhausted after per-model mark")
	}
	if ec.IsExhaustedScope("conn-1", "family:claude") {
		t.Error("family:claude should not be exhausted")
	}
	if !ec.IsExhaustedScope("conn-1", "family:gemini") {
		t.Error("family:gemini should be exhausted")
	}

	// Empty scope resolves to the connection-wide key.
	if key := ExhaustKey("conn-1", ""); key != "conn-1" {
		t.Errorf("ExhaustKey(conn-1, \"\")=%q, want conn-1", key)
	}

	// Clearing the scoped key should leave the connection-wide key alone.
	ec.Clear(key)
	if ec.IsExhaustedScope("conn-1", "family:gemini") {
		t.Error("family:gemini should be cleared")
	}
}

func TestExhaustion_ConnectionWideKey(t *testing.T) {
	ec := NewExhaustionCache()

	ec.MarkExhausted("conn-2", 5*time.Minute)

	if !ec.IsExhausted("conn-2") {
		t.Error("connection-wide key should be exhausted")
	}
	if ec.IsExhaustedScope("conn-2", "any-model") {
		t.Error("per-model scope on a connection-only mark must not be exhausted")
	}
}

func TestExhaustion_IsExhaustedAt(t *testing.T) {
	ec := NewExhaustionCache()
	now := time.Now()

	ec.MarkExhausted("conn-3", 5*time.Minute)

	if !ec.IsExhaustedAt("conn-3", now) {
		t.Error("key should be exhausted at now")
	}
	if ec.IsExhaustedScopeAt("conn-3", "family:gemini", now) {
		t.Error("unscoped key should not make scoped key exhausted")
	}

	key := ExhaustKey("conn-4", "family:gemini")
	ec.MarkExhausted(key, 5*time.Minute)
	if !ec.IsExhaustedAt(key, now) {
		t.Error("scoped key should be exhausted at now")
	}
	if ec.IsExhaustedAt("conn-4", now) {
		t.Error("connection-wide key should not be exhausted after scoped mark")
	}

	later := now.Add(10 * time.Minute)
	if ec.IsExhaustedAt("conn-3", later) {
		t.Error("key should be expired at later time")
	}
	if ec.IsExhaustedScopeAt("conn-4", "family:gemini", later) {
		t.Error("scoped key should be expired at later time")
	}
}

func TestExhaustion_IsExhausted_DelegatesToAt(t *testing.T) {
	ec := NewExhaustionCache()
	ec.MarkExhausted("conn-delegates", 5*time.Minute)
	if !ec.IsExhausted("conn-delegates") {
		t.Error("IsExhausted should delegate to IsExhaustedAt with current time")
	}
}
