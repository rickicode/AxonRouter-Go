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

func TestConnectionState_ResetQuota_ClearsCooldownAndReturnsAffectedModels(t *testing.T) {
	cs := &ConnectionState{ID: "c1"}
	now := time.Now()
	future := now.Add(time.Minute)
	past := now.Add(-time.Minute)

	cs.SetCooldown(future)
	cs.SetModelCooldown("gpt-4o", future)
	cs.SetModelCooldown("expired", past)
	cs.SetStatus(StatusQuotaExhausted, "hit quota")
	cs.FailCount = 3
	cs.BanCount = 2

	updated, affected, err := cs.ResetQuota()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated != cs {
		t.Error("expected ResetQuota to return the same connection state")
	}
	if updated.GetStatus() != StatusReady {
		t.Errorf("expected status ready, got %s", updated.GetStatus())
	}
	if updated.IsInCooldownAt(now) {
		t.Error("expected connection cooldown to be cleared")
	}
	if updated.IsModelInCooldownAt("gpt-4o", now) {
		t.Error("expected gpt-4o cooldown to be cleared")
	}
	if len(affected) != 1 || affected[0] != "gpt-4o" {
		t.Errorf("expected only gpt-4o affected, got %v", affected)
	}
	if updated.FailCount != 0 || updated.BanCount != 0 {
		t.Errorf("expected fail/ban counts reset, got fail=%d ban=%d", updated.FailCount, updated.BanCount)
	}
}

func TestConnectionState_ResetQuota_KeepsDisabled(t *testing.T) {
	cs := &ConnectionState{ID: "c1"}
	cs.SetStatus(StatusDisabled, "manual")

	updated, affected, err := cs.ResetQuota()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.GetStatus() != StatusDisabled {
		t.Errorf("expected disabled status to remain, got %s", updated.GetStatus())
	}
	if len(affected) != 0 {
		t.Errorf("expected no affected models, got %v", affected)
	}
}
