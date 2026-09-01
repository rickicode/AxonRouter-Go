package proxypool

import (
	"encoding/json"
	"testing"
	"time"
)

// fakeSettingsDB is a minimal in-memory settings store for fitness tests.
type fakeSettingsDB struct {
	values map[string]string
}

func newFakeSettingsDB() *fakeSettingsDB {
	return &fakeSettingsDB{values: map[string]string{}}
}

func (f *fakeSettingsDB) GetSetting(key, defaultVal string) string {
	if v, ok := f.values[key]; ok && v != "" {
		return v
	}
	return defaultVal
}

func (f *fakeSettingsDB) SetSetting(key, value string) error {
	f.values[key] = value
	return nil
}

// newTestFitness creates a registry backed by a fake settings store.
func newTestFitness() (*FitnessRegistry, *fakeSettingsDB) {
	s := newFakeSettingsDB()
	f := NewFitnessRegistry()
	f.db = s
	return f, s
}

func TestFitnessMarkAndIsFit(t *testing.T) {
	f, _ := newTestFitness()
	f.MarkUnfit("pool-1", ScopeFor("freebuff", "gpt-4o"), "limited_ip", 5*time.Minute)

	if f.IsFit("pool-1", ScopeFor("freebuff", "gpt-4o")) {
		t.Error("pool-1 should be unfit for freebuff::gpt-4o")
	}
	if !f.IsFit("pool-1", ScopeFor("freebuff", "gpt-5")) {
		t.Error("pool-1 should be fit for a different model")
	}
	if !f.IsFit("pool-2", ScopeFor("freebuff", "gpt-4o")) {
		t.Error("pool-2 should be fit (never marked)")
	}
}

func TestFitnessProviderWildcard(t *testing.T) {
	f, _ := newTestFitness()
	f.MarkUnfit("pool-1", ScopeFor("freebuff", "*"), "provider_block", 5*time.Minute)

	if f.IsFit("pool-1", ScopeFor("freebuff", "gpt-4o")) {
		t.Error("provider wildcard should make pool unfit for any freebuff model")
	}
	if f.IsFit("pool-1", ScopeFor("freebuff", "anything")) {
		t.Error("provider wildcard should match arbitrary model")
	}
	if !f.IsFit("pool-1", ScopeFor("openai", "gpt-4o")) {
		t.Error("wildcard must be provider-scoped; openai should be fit")
	}
}

func TestFitnessExpiry(t *testing.T) {
	f, _ := newTestFitness()
	f.MarkUnfit("pool-1", ScopeFor("freebuff", "gpt-4o"), "limited_ip", 50*time.Millisecond)

	if f.IsFit("pool-1", ScopeFor("freebuff", "gpt-4o")) {
		t.Fatal("pool should be unfit before expiry")
	}
	time.Sleep(80 * time.Millisecond)
	if !f.IsFit("pool-1", ScopeFor("freebuff", "gpt-4o")) {
		t.Error("pool should be fit again after expiry")
	}
}

func TestFitnessFitIDs(t *testing.T) {
	f, _ := newTestFitness()
	f.MarkUnfit("pool-2", ScopeFor("freebuff", "gpt-4o"), "limited_ip", 5*time.Minute)

	ids := []string{"pool-1", "pool-2", "pool-3"}
	fit := f.FitIDs(ids, ScopeFor("freebuff", "gpt-4o"))
	if len(fit) != 2 || fit[0] != "pool-1" || fit[1] != "pool-3" {
		t.Errorf("FitIDs = %v, want [pool-1 pool-3]", fit)
	}

	// When nothing is filtered, the original slice must be returned.
	ids2 := []string{"pool-1", "pool-3"}
	fit2 := f.FitIDs(ids2, ScopeFor("freebuff", "gpt-4o"))
	if &fit2[0] != &ids2[0] {
		t.Error("FitIDs should return the original slice when unchanged")
	}
}

func TestFitnessClear(t *testing.T) {
	f, _ := newTestFitness()
	f.MarkUnfit("pool-1", ScopeFor("freebuff", "gpt-4o"), "limited_ip", 5*time.Minute)
	f.MarkUnfit("pool-1", ScopeFor("freebuff", "gpt-5"), "limited_ip", 5*time.Minute)

	// Clear a single scope.
	f.Clear("pool-1", ScopeFor("freebuff", "gpt-4o"))
	if !f.IsFit("pool-1", ScopeFor("freebuff", "gpt-4o")) {
		t.Error("scope should be fit after Clear")
	}
	if f.IsFit("pool-1", ScopeFor("freebuff", "gpt-5")) {
		t.Error("other scope should still be unfit")
	}

	// Clear all scopes for the pool.
	f.MarkUnfit("pool-1", ScopeFor("freebuff", "gpt-4o"), "limited_ip", 5*time.Minute)
	f.Clear("pool-1", "")
	if !f.IsFit("pool-1", ScopeFor("freebuff", "gpt-5")) {
		t.Error("Clear with empty scope should remove all marks for the pool")
	}
}

func TestFitnessClearAll(t *testing.T) {
	f, _ := newTestFitness()
	f.MarkUnfit("pool-1", ScopeFor("freebuff", "gpt-4o"), "limited_ip", 5*time.Minute)
	f.MarkUnfit("pool-2", ScopeFor("openai", "gpt-4o"), "limited_ip", 5*time.Minute)

	f.ClearAll()
	if len(f.Snapshot()) != 0 {
		t.Errorf("Snapshot after ClearAll = %v, want empty", f.Snapshot())
	}
}

func TestFitnessSnapshot(t *testing.T) {
	f, _ := newTestFitness()
	f.MarkUnfit("pool-1", ScopeFor("freebuff", "gpt-4o"), "limited_ip", 5*time.Minute)

	snap := f.Snapshot()
	m, ok := snap["pool-1"]
	if !ok {
		t.Fatal("snapshot missing pool-1")
	}
	if m[ScopeFor("freebuff", "gpt-4o")].Reason != "limited_ip" {
		t.Errorf("snapshot reason = %q, want limited_ip", m[ScopeFor("freebuff", "gpt-4o")].Reason)
	}

	// Snapshot must be a deep copy.
	m[ScopeFor("freebuff", "gpt-4o")] = FitnessMark{}
	if got := f.Snapshot()["pool-1"][ScopeFor("freebuff", "gpt-4o")].Reason; got != "limited_ip" {
		t.Errorf("snapshot is not a deep copy: reason = %q", got)
	}
}

func TestFitnessPersistRoundTrip(t *testing.T) {
	f, store := newTestFitness()
	f.MarkUnfit("pool-1", ScopeFor("freebuff", "gpt-4o"), "limited_ip", 5*time.Minute)

	// Force the debounced persist to run synchronously.
	f.persist()

	raw, ok := store.values["proxy_pool_fitness"]
	if !ok || raw == "" {
		t.Fatal("expected proxy_pool_fitness setting to be written")
	}
	var decoded map[string]map[string]FitnessMark
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("persisted value is not valid JSON: %v", err)
	}

	// A fresh registry hydrating from the store must see the mark (unfit).
	f2 := NewFitnessRegistry()
	f2.db = store
	if f2.IsFit("pool-1", ScopeFor("freebuff", "gpt-4o")) {
		t.Error("fresh registry should hydrate the persisted mark")
	}
	if !f2.IsFit("pool-1", ScopeFor("freebuff", "gpt-5")) {
		t.Error("hydrated mark must be scope-specific — gpt-5 should be fit")
	}
}

func TestFitnessEmptyPoolIDIgnored(t *testing.T) {
	f, _ := newTestFitness()
	f.MarkUnfit("", ScopeFor("freebuff", "gpt-4o"), "limited_ip", 5*time.Minute)
	if len(f.Snapshot()) != 0 {
		t.Error("MarkUnfit with empty poolID must be a no-op")
	}
	if !f.IsFit("", ScopeFor("freebuff", "gpt-4o")) {
		t.Error("IsFit with empty poolID must be fit")
	}
}

func TestFitnessHydrateCorrupt(t *testing.T) {
	store := newFakeSettingsDB()
	store.values["proxy_pool_fitness"] = "{not-json"

	f := NewFitnessRegistry()
	f.db = store
	// Must not panic, must treat as no marks.
	if !f.IsFit("pool-1", ScopeFor("freebuff", "gpt-4o")) {
		t.Error("corrupt persisted data should fall back to fit")
	}
}