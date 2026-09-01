package proxypool

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/db"
)

// FitnessMark records a single unfitness event for a pool+scope pair.
type FitnessMark struct {
	Until  time.Time `json:"until"`
	Reason string    `json:"reason"`
}

// FitnessRegistry tracks pool-fitness marks keyed by pool ID and scope.
// A mark signals that a pool's egress is "unfit" for a given provider::model
// scope (e.g. because a provider IP-limits that proxy's egress). Expired marks
// are pruned on read. Marks are persisted to the settings table as JSON and
// survive restart.
//
// The zero value is NOT ready for use — call NewFitnessRegistry() or rely on
// the package singleton Fitness().
type FitnessRegistry struct {
	mu          sync.RWMutex
	marks       map[string]map[string]FitnessMark // poolID -> scope -> mark
	persistTimer *time.Timer
	hydrated    bool
	db          interface {
		GetSetting(key, defaultVal string) string
		SetSetting(key, value string) error
	}
}

// Fitness returns the package-level singleton FitnessRegistry.
func Fitness() *FitnessRegistry {
	singletonInit.Do(func() {
		singleton = NewFitnessRegistry()
	})
	return singleton
}

var (
	singleton     *FitnessRegistry
	singletonInit sync.Once
)

// NewFitnessRegistry creates a standalone FitnessRegistry for tests or prod.
func NewFitnessRegistry() *FitnessRegistry {
	return &FitnessRegistry{
		marks: map[string]map[string]FitnessMark{},
		db:    dbAdapter{},
	}
}

// dbAdapter wraps the global db package so tests can inject a mock.
type dbAdapter struct{}

func (dbAdapter) GetSetting(key, defaultVal string) string { return db.GetSetting(key, defaultVal) }
func (dbAdapter) SetSetting(key, value string) error       { return db.SetSetting(key, value) }

// ScopeFor returns the scope string for a provider::model pair.
func ScopeFor(provider, model string) string {
	return provider + "::" + model
}

// MarkUnfit records that poolID is unfit for the given scope until
// time.Now().Add(ttl). Triggers a debounced persist.
func (f *FitnessRegistry) MarkUnfit(poolID, scope, reason string, ttl time.Duration) {
	if poolID == "" {
		return
	}
	f.hydrate()
	f.mu.Lock()
	defer f.mu.Unlock()

	marks, ok := f.marks[poolID]
	if !ok {
		marks = map[string]FitnessMark{}
		f.marks[poolID] = marks
	}
	marks[scope] = FitnessMark{
		Until:  time.Now().Add(ttl),
		Reason: reason,
	}
	f.schedulePersistLocked()
}

// Clear removes the mark for poolID+scope. If scope is "", all marks for the
// pool are removed. Triggers a debounced persist.
func (f *FitnessRegistry) Clear(poolID, scope string) {
	if poolID == "" {
		return
	}
	f.hydrate()
	f.mu.Lock()
	defer f.mu.Unlock()

	if scope == "" {
		delete(f.marks, poolID)
	} else {
		if m, ok := f.marks[poolID]; ok {
			delete(m, scope)
			if len(m) == 0 {
				delete(f.marks, poolID)
			}
		}
	}
	f.schedulePersistLocked()
}

// ClearAll removes every fitness mark. Triggers a debounced persist.
func (f *FitnessRegistry) ClearAll() {
	f.hydrate()
	f.mu.Lock()
	defer f.mu.Unlock()

	for poolID := range f.marks {
		delete(f.marks, poolID)
	}
	f.schedulePersistLocked()
}

// IsFit reports whether poolID is fit (not marked) for the given scope.
// It checks the exact scope match first, then falls back to a provider-wide
// wildcard (provider::*). Expired marks are pruned during the check.
func (f *FitnessRegistry) IsFit(poolID, scope string) bool {
	if poolID == "" {
		return true
	}
	f.hydrate()
	f.mu.RLock()
	defer f.mu.RUnlock()

	marks, ok := f.marks[poolID]
	if !ok {
		return true
	}

	// Extract the provider prefix from scope ("freebuff::gpt-4o" -> "freebuff::*")
	providerWildcard := ""
	if idx := indexOfN(scope, "::", 1); idx > 0 {
		providerWildcard = scope[:idx] + "::*"
	}

	now := time.Now()
	for s, mark := range marks {
		if now.Before(mark.Until) {
			// Active mark — check if it matches the requested scope.
			if s == scope || (providerWildcard != "" && s == providerWildcard) {
				return false
			}
		}
		// Expired marks are pruned during the write lock below.
	}
	return true
}

// FitIDs filters the id slice to only those that are fit for scope.
// Returns the original slice when no ids are filtered out.
func (f *FitnessRegistry) FitIDs(ids []string, scope string) []string {
	f.hydrate()
	f.mu.RLock()

	// Quick prune-expired while we have the read lock; accumulated expired
	// marks hurt scan performance. We do the cheapest check first.
	var needsPrune bool
	now := time.Now()
	for _, marks := range f.marks {
		for _, m := range marks {
			if !now.Before(m.Until) {
				needsPrune = true
				break
			}
		}
		if needsPrune {
			break
		}
	}
	if needsPrune {
		f.mu.RUnlock()
		f.mu.Lock()
		f.pruneLocked(now)
		f.mu.Unlock()
		f.mu.RLock()
	}

	providerWildcard := ""
	if idx := indexOfN(scope, "::", 1); idx > 0 {
		providerWildcard = scope[:idx] + "::*"
	}

	fit := make([]string, 0, len(ids))
	for _, id := range ids {
		marks, ok := f.marks[id]
		if !ok {
			fit = append(fit, id)
			continue
		}
		unfit := false
		for scopeKey, m := range marks {
			if now.Before(m.Until) && (scopeKey == scope || (providerWildcard != "" && scopeKey == providerWildcard)) {
				unfit = true
				break
			}
		}
		if !unfit {
			fit = append(fit, id)
		}
	}
	f.mu.RUnlock()

	if len(fit) == len(ids) {
		return ids // unchanged, return original
	}
	return fit
}

// Snapshot returns a deep copy of all marks, poolID -> scope -> mark.
func (f *FitnessRegistry) Snapshot() map[string]map[string]FitnessMark {
	f.hydrate()
	f.mu.RLock()
	defer f.mu.RUnlock()

	out := make(map[string]map[string]FitnessMark, len(f.marks))
	for pid, sm := range f.marks {
		scopes := make(map[string]FitnessMark, len(sm))
		for s, m := range sm {
			scopes[s] = m
		}
		out[pid] = scopes
	}
	return out
}

// hydrate reads persisted marks from the settings table on first access.
// Safe to call multiple times; idempotent after the first real hydration.
func (f *FitnessRegistry) hydrate() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hydrated {
		return
	}
	f.hydrated = true
	raw := f.db.GetSetting("proxy_pool_fitness", "")
	if raw == "" {
		return
	}
	var stored map[string]map[string]FitnessMark
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return
	}
	f.marks = stored
}

// schedulePersistLocked triggers a debounced settings write. Caller must hold f.mu.
func (f *FitnessRegistry) schedulePersistLocked() {
	if f.persistTimer != nil {
		f.persistTimer.Stop()
	}
	f.persistTimer = time.AfterFunc(2*time.Second, func() {
		f.persist()
	})
}

// persist marshals the current marks to JSON and writes them to the settings table.
func (f *FitnessRegistry) persist() {
	f.mu.RLock()
	data, err := json.Marshal(f.marks)
	f.mu.RUnlock()
	if err != nil {
		return
	}
	_ = f.db.SetSetting("proxy_pool_fitness", string(data))
}

// pruneLocked removes expired marks. Caller must hold f.mu (write lock).
func (f *FitnessRegistry) pruneLocked(now time.Time) {
	for poolID, marks := range f.marks {
		for s, m := range marks {
			if !now.Before(m.Until) {
				delete(marks, s)
			}
		}
		if len(marks) == 0 {
			delete(f.marks, poolID)
		}
	}
}

// indexOfN returns the index of the nth occurrence of substr in s, or -1.
func indexOfN(s, substr string, n int) int {
	idx := -1
	for i := 0; i < n; i++ {
		prev := idx + 1
		if prev >= len(s) {
			return -1
		}
		idx = strings.Index(s[prev:], substr)
		if idx < 0 {
			return -1
		}
		idx += prev
	}
	return idx
}