package connstate

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// EligibilitySnapshot is an immutable set of eligible provider IDs for fast routing.
type EligibilitySnapshot struct {
	Providers     []string
	ByPrefix      map[string][]string
	ByPrefixState map[string][]*ConnectionState
}

// EligibilityManager manages eligibility snapshots for O(1) routing decisions.
type EligibilityManager struct {
	store     *Store
	snapshot  atomic.Value // *EligibilitySnapshot
	updateMu  sync.Mutex    // guards coalescing window state
	updateScheduled bool    // true while a coalesced Update is pending
	lastUpdate time.Time    // timestamp of last actual rebuild
}

// updateCoalesceWindow bounds how often the O(N) snapshot rebuild runs when
// triggered by concurrent failovers. Multiple status changes within this window
// collapse into a single rebuild, preventing O(N) CPU spikes under bursty
// 429/5xx load (hundreds of concurrent failovers would otherwise each scan all
// connections + shuffle). Latency for status propagation stays ≤ this window.
const updateCoalesceWindow = 50 * time.Millisecond

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
// Connections are ordered by remaining quota (highest first) within each prefix,
// so routing modes such as round_robin and first_eligible prefer healthy
// accounts while still rotating fallback across siblings.
func (e *EligibilityManager) Update(store *Store) {
	eligibleStates := make(map[string][]*ConnectionState)
	var allStates []*ConnectionState

	store.RangeByConnID(func(connID string, cs *ConnectionState) bool {
		status := cs.GetStatus()
		// Exclude connections that are actively cooled down or exhausted.  This keeps
		// the eligibility snapshot consistent with getConnection's preflight checks
		// and guarantees we never route to rate-limited/exhausted accounts.
		if status != StatusReady && status != StatusDegraded {
			return true
		}
		if cs.IsInCooldown() {
			return true
		}
		allStates = append(allStates, cs)
		prefix := cs.Prefix
		eligibleStates[prefix] = append(eligibleStates[prefix], cs)
		return true
	})

	// Order each prefix by remaining quota (highest first), then by recency
	// (least-recently-used first). Routing modes then rotate or pick a random
	// start on this list, naturally preferring healthy accounts while spreading
	// simultaneous requests across siblings instead of concentrating on the
	// same freshly-selected connection.
	for _, conns := range eligibleStates {
		sort.SliceStable(conns, func(i, j int) bool {
			ri, rj := conns[i].GetRemainingPct(), conns[j].GetRemainingPct()
			if ri != rj {
				return ri > rj
			}
			return conns[i].lastUsedAtNano() < conns[j].lastUsedAtNano()
		})
	}

	// Build string slices from the sorted pointer slices for callers that only
	// need connection IDs. Both views share the same ordering.
	eligible := make(map[string][]string, len(eligibleStates))
	for prefix, conns := range eligibleStates {
		ids := make([]string, len(conns))
		for i, cs := range conns {
			ids[i] = cs.ID
		}
		eligible[prefix] = ids
	}
	providers := make([]string, 0, len(allStates))
	for _, cs := range allStates {
		providers = append(providers, cs.ID)
	}

	e.snapshot.Store(&EligibilitySnapshot{
		Providers:     providers,
		ByPrefix:      eligible,
		ByPrefixState: eligibleStates,
	})
}

// ScheduleUpdate coalesces concurrent eligibility rebuild requests into a single
// O(N) rebuild per updateCoalesceWindow. This is called on every status change
// (failover error, ban→disabled, recovery). Under bursty failover load (hundreds
// of concurrent 429s), all the status changes within a 50ms window collapse into
// ONE rebuild instead of N, bounding CPU cost. Status propagation latency stays
// ≤ updateCoalesceWindow. The immediate Update() is still used by admin/background
// paths that need a guaranteed synchronous rebuild.
func (e *EligibilityManager) ScheduleUpdate() {
	e.updateMu.Lock()
	if e.updateScheduled {
		// Another goroutine already owns the window — skip.
		e.updateMu.Unlock()
		return
	}
	if time.Since(e.lastUpdate) >= updateCoalesceWindow {
		// Enough time has passed — rebuild immediately (no goroutine, no delay).
		e.lastUpdate = time.Now()
		e.updateMu.Unlock()
		e.Update(e.store)
		return
	}
	// Within the window — mark pending and spawn a coalescing rebuild.
	e.updateScheduled = true
	e.updateMu.Unlock()
	go func() {
		time.Sleep(updateCoalesceWindow)
		e.updateMu.Lock()
		e.updateScheduled = false
		e.lastUpdate = time.Now()
		e.updateMu.Unlock()
		e.Update(e.store)
	}()
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

// GetByPrefixState returns eligible connection states for a specific prefix.
// The slice is pre-sorted by remaining quota (highest first) and is safe for
// lock-free use on the routing hot path.
func (e *EligibilityManager) GetByPrefixState(prefix string) []*ConnectionState {
	snap := e.Get()
	if snap.ByPrefixState == nil {
		return nil
	}
	return snap.ByPrefixState[prefix]
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
