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
