package connstate

import (
	"sort"
	"sync/atomic"
)

// EligibilitySnapshot is an immutable set of eligible provider IDs for fast routing.
type EligibilitySnapshot struct {
	Providers []string
	ByPrefix  map[string][]string
}

// EligibilityManager manages eligibility snapshots for O(1) routing decisions.
type EligibilityManager struct {
	store    *Store
	snapshot atomic.Value // *EligibilitySnapshot
}

// NewEligibilityManager creates a new eligibility manager.
func NewEligibilityManager(store *Store) *EligibilityManager {
	e := &EligibilityManager{
		store: store,
	}
	e.snapshot.Store(&EligibilitySnapshot{
		Providers: nil,
		ByPrefix:  make(map[string][]string),
	})
	return e
}

// Update recomputes the eligibility snapshot from the current store state.
// Connections are sorted by priority (higher first) within each prefix.
func (e *EligibilityManager) Update(store *Store) {
	eligible := make(map[string][]string)
	var all []string

	store.RangeByConnID(func(connID string, cs *ConnectionState) bool {
		status := cs.GetStatus()
		if status == StatusReady || status == StatusDegraded {
			all = append(all, connID)
			prefix := cs.Prefix
			eligible[prefix] = append(eligible[prefix], connID)
		}
		return true
	})

	// Sort each prefix's connections by priority (higher first)
	for _, ids := range eligible {
		sort.SliceStable(ids, func(i, j int) bool {
			ci := store.Get(ids[i])
			cj := store.Get(ids[j])
			if ci == nil || cj == nil {
				return false
			}
			return ci.GetPriority() > cj.GetPriority()
		})
	}

	e.snapshot.Store(&EligibilitySnapshot{
		Providers: all,
		ByPrefix:  eligible,
	})
}

// Get returns the current eligibility snapshot (lock-free).
func (e *EligibilityManager) Get() *EligibilitySnapshot {
	return e.snapshot.Load().(*EligibilitySnapshot)
}

// GetByPrefix returns eligible connection IDs for a specific prefix.
func (e *EligibilityManager) GetByPrefix(prefix string) []string {
	snap := e.Get()
	if snap.ByPrefix == nil {
		return nil
	}
	return snap.ByPrefix[prefix]
}

// GetAll returns all eligible connection IDs.
func (e *EligibilityManager) GetAll() []string {
	snap := e.Get()
	return snap.Providers
}

// IsEligible checks if a specific connection is currently eligible.
func (e *EligibilityManager) IsEligible(connID string) bool {
	snap := e.Get()
	for _, id := range snap.Providers {
		if id == connID {
			return true
		}
	}
	return false
}

// RecomputeAll recomputes eligibility from the store.
func (e *EligibilityManager) RecomputeAll() {
	if e.store != nil {
		e.Update(e.store)
	}
}

// PickConnection picks an eligible connection for a prefix and model.
// Checks both connection-level and model-level cooldowns.
func (e *EligibilityManager) PickConnection(prefix, modelID string) *ConnectionState {
	conns := e.GetByPrefix(prefix)
	if len(conns) == 0 {
		return nil
	}

	// Find first connection where model is not in cooldown
	for _, connID := range conns {
		cs := e.store.Get(connID)
		if cs == nil {
			continue
		}
		// Check connection-level cooldown
		if cs.IsInCooldown() {
			continue
		}
		// Check model-level cooldown
		if modelID != "" && cs.IsModelInCooldown(modelID) {
			continue
		}
		return cs
	}

	// Fallback: return first eligible (even if in cooldown)
	return &ConnectionState{ID: conns[0]}
}
