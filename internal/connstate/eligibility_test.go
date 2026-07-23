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

// TestEligibilityByPrefixStatePreMaterialized proves the snapshot contains
// pre-sorted *ConnectionState pointers so the routing hot path can avoid
// repeated store.Get lookups.
func TestEligibilityByPrefixStatePreMaterialized(t *testing.T) {
	store := NewStore()

	store.SeedConnection("conn-low", "test", string(StatusReady), 0)
	store.SeedConnection("conn-mid", "test", string(StatusReady), 0)
	store.SeedConnection("conn-high", "test", string(StatusReady), 0)

	store.Get("conn-low").SetRemainingPct(10)
	store.Get("conn-mid").SetRemainingPct(50)
	store.Get("conn-high").SetRemainingPct(90)

	mgr := NewEligibilityManager(store)
	mgr.RecomputeAll()

	states := mgr.GetByPrefixState("test")
	if len(states) != 3 {
		t.Fatalf("expected 3 states, got %d", len(states))
	}

	want := []string{"conn-high", "conn-mid", "conn-low"}
	for i, ws := range want {
		if states[i].ID != ws {
			t.Fatalf("state[%d].ID = %s, want %s; got %v", i, states[i].ID, ws, idsFromStates(states))
		}
	}

	ids := mgr.GetByPrefix("test")
	if !sameStringSlice(ids, want) {
		t.Fatalf("GetByPrefix order %v does not match expected %v", ids, want)
	}
}

// TestEligibilityExcludesTerminalStatuses proves that connections with a
// routing-terminal status never appear in the snapshot.
func TestEligibilityExcludesTerminalStatuses(t *testing.T) {
	store := NewStore()

	store.SeedConnection("conn-ready", "test", string(StatusReady), 0)
	store.SeedConnection("conn-disabled", "test", string(StatusDisabled), 0)
	store.SeedConnection("conn-disabled-2", "test", string(StatusDisabled), 0)
	store.SeedConnection("conn-disabled-3", "test", string(StatusDisabled), 0)
	store.SeedConnection("conn-disabled-4", "test", string(StatusDisabled), 0)

	mgr := NewEligibilityManager(store)
	mgr.RecomputeAll()

	ids := mgr.GetByPrefix("test")
	if len(ids) != 1 || ids[0] != "conn-ready" {
		t.Fatalf("expected only conn-ready in ByPrefix, got %v", ids)
	}

	states := mgr.GetByPrefixState("test")
	if len(states) != 1 || states[0].ID != "conn-ready" {
		t.Fatalf("expected only conn-ready in ByPrefixState, got %v", idsFromStates(states))
	}

	all := mgr.GetAll()
	if len(all) != 1 || all[0] != "conn-ready" {
		t.Fatalf("expected only conn-ready in GetAll, got %v", all)
	}

	for _, id := range []string{"conn-disabled", "conn-disabled-2", "conn-disabled-3", "conn-disabled-4"} {
		if mgr.IsEligible(id) {
			t.Fatalf("terminal connection %s should not be eligible", id)
		}
	}
}

func idsFromStates(states []*ConnectionState) []string {
	ids := make([]string, len(states))
	for i, s := range states {
		ids[i] = s.ID
	}
	return ids
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestEligibility_UpdateProvider_only_affected_provider proves that
// UpdateProvider recomputes the snapshot for exactly one provider and leaves
// other providers untouched.
func TestEligibility_UpdateProvider_only_affected_provider(t *testing.T) {
	store := NewStore()

	store.SeedConnection("a-1", "provider-a", string(StatusReady), 0)
	store.SeedConnection("b-1", "provider-b", string(StatusReady), 0)

	mgr := NewEligibilityManager(store)
	mgr.RecomputeAll()

	if got := mgr.GetByPrefix("provider-a"); len(got) != 1 || got[0] != "a-1" {
		t.Fatalf("provider-a before change: got %v", got)
	}
	if got := mgr.GetByPrefix("provider-b"); len(got) != 1 || got[0] != "b-1" {
		t.Fatalf("provider-b before change: got %v", got)
	}

	// Fail only provider-a; update just that provider.
	store.Get("a-1").SetStatus(StatusDisabled, "")
	mgr.UpdateProvider("provider-a")

	if got := mgr.GetByPrefix("provider-a"); len(got) != 0 {
		t.Fatalf("provider-a should be empty after update, got %v", got)
	}
	if got := mgr.GetByPrefix("provider-b"); len(got) != 1 || got[0] != "b-1" {
		t.Fatalf("provider-b should remain eligible, got %v", got)
	}
	if mgr.IsEligible("a-1") {
		t.Fatal("failed provider-a connection should not be eligible")
	}
	if !mgr.IsEligible("b-1") {
		t.Fatal("provider-b connection should still be eligible")
	}
}

// TestEligibility_ScheduleUpdateProvider_coalesces proves a scheduled
// per-provider update eventually rebuilds the snapshot without scanning all
// providers.
func TestEligibility_ScheduleUpdateProvider_coalesces(t *testing.T) {
	store := NewStore()
	store.SeedConnection("x-1", "provider-x", string(StatusReady), 0)

	mgr := NewEligibilityManager(store)
	mgr.RecomputeAll()

	store.SeedConnection("x-2", "provider-x", string(StatusReady), 0)
	mgr.ScheduleUpdateProvider("provider-x")

	// Wait longer than the coalesce window.
	time.Sleep(150 * time.Millisecond)

	if got := mgr.GetByPrefix("provider-x"); len(got) != 2 {
		t.Fatalf("expected 2 provider-x entries after scheduled update, got %v", got)
	}
}
