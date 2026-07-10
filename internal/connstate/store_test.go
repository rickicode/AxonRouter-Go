package connstate

import (
	"testing"
	"time"
)

func TestRecordFailure_ModelCooldown(t *testing.T) {
	store := NewStore()
	store.SeedConnection("conn-1", "openai", "ready", 0)

	until := time.Now().Add(time.Minute)
	det := ErrorDetection{
		Category:      ErrorRateLimit,
		Status:        StatusRateLimited,
		Scope:         "model",
		ModelID:       "gpt-4o",
		CooldownUntil: &until,
	}
	store.RecordFailure("conn-1", det)

	cs := store.Get("conn-1")
	if cs == nil {
		t.Fatal("connection state not found")
	}
	if !cs.IsModelInCooldown("gpt-4o") {
		t.Error("expected gpt-4o to be in cooldown")
	}
	if cs.IsModelInCooldown("gpt-3.5-turbo") {
		t.Error("did not expect gpt-3.5-turbo to be in cooldown")
	}
}

func TestRecordFailure_ConnectionCooldown(t *testing.T) {
	store := NewStore()
	store.SeedConnection("conn-1", "openai", "ready", 0)

	until := time.Now().Add(time.Minute)
	det := ErrorDetection{
		Category:      ErrorRateLimit,
		Status:        StatusRateLimited,
		Scope:         "connection",
		CooldownUntil: &until,
	}
	store.RecordFailure("conn-1", det)

	cs := store.Get("conn-1")
	if cs == nil {
		t.Fatal("connection state not found")
	}
	if !cs.IsInCooldown() {
		t.Error("expected connection to be in cooldown")
	}
}
