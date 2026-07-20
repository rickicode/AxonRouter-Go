package connstate

import (
	"testing"
	"time"
)

func TestConnectionState_IsInCooldownAt(t *testing.T) {
	cs := &ConnectionState{}

	now := time.Now()
	if cs.IsInCooldownAt(now) {
		t.Error("fresh connection should not be in cooldown")
	}

	future := now.Add(time.Minute)
	cs.SetCooldown(future)
	if !cs.IsInCooldownAt(now) {
		t.Error("expected cooldown to be active at now when until is in the future")
	}

	past := now.Add(2 * time.Minute)
	if cs.IsInCooldownAt(past) {
		t.Error("expected cooldown to be expired when now is after until")
	}
}

func TestConnectionState_IsModelInCooldownAt(t *testing.T) {
	cs := &ConnectionState{}
	now := time.Now()

	if cs.IsModelInCooldownAt("gpt-4o", now) {
		t.Error("fresh model should not be in cooldown")
	}

	future := now.Add(time.Minute)
	cs.SetModelCooldown("gpt-4o", future)
	if !cs.IsModelInCooldownAt("gpt-4o", now) {
		t.Error("expected model to be in cooldown after SetModelCooldown")
	}
	if cs.IsModelInCooldownAt("gpt-3.5-turbo", now) {
		t.Error("different model should not be in cooldown")
	}

	past := now.Add(2 * time.Minute)
	if cs.IsModelInCooldownAt("gpt-4o", past) {
		t.Error("expected model cooldown to be expired when now is after until")
	}
}

func TestModelLimitState_IsInCooldownAt(t *testing.T) {
	mls := &ModelLimitState{ModelID: "m"}
	now := time.Now()

	if mls.IsInCooldownAt(now) {
		t.Error("fresh ModelLimitState should not be in cooldown")
	}

	future := now.Add(time.Minute)
	mls.SetCooldown(future)
	if !mls.IsInCooldownAt(now) {
		t.Error("expected model cooldown to be active")
	}

	past := now.Add(2 * time.Minute)
	if mls.IsInCooldownAt(past) {
		t.Error("expected model cooldown to be expired")
	}
}

func TestConnectionState_IsInCooldown_DelegatesToAt(t *testing.T) {
	cs := &ConnectionState{}
	future := time.Now().Add(time.Minute)
	cs.SetCooldown(future)
	if !cs.IsInCooldown() {
		t.Error("IsInCooldown should delegate to IsInCooldownAt(time.Now())")
	}
}
