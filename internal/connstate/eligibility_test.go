package connstate

import (
	"testing"
	"time"
)

// TestEligibilityExcludesCooldownConnections proves that actively-cooled-down
// connections never appear in the eligibility snapshot, even if their status is
// ready or degraded.
func TestEligibilityExcludesCooldownConnections(t *testing.T) {
	store := NewStore()
	future := time.Now().Add(time.Hour)

	store.SeedConnection("conn-ready", "test", string(StatusReady), 0)
	store.SeedConnection("conn-cooled", "test", string(StatusReady), 0)
	store.Get("conn-cooled").SetCooldown(future)

	mgr := NewEligibilityManager(store)
	mgr.RecomputeAll()

	ids := mgr.GetByPrefix("test")
	if len(ids) != 1 || ids[0] != "conn-ready" {
		t.Fatalf("expected only conn-ready, got %v", ids)
	}
	if mgr.IsEligible("conn-cooled") {
		t.Fatal("cooled connection should not be eligible")
	}
}

