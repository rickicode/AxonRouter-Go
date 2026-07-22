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

// providerSnapshot holds the eligible connection state for a single provider.
// It is immutable after publication and safe for lock-free reads via atomic.Value.
type providerSnapshot struct {
	States []*ConnectionState
	IDs    []string
}

// EligibilityManager manages eligibility snapshots for O(1) routing decisions.
type EligibilityManager struct {
	store      *Store
	snapshot   atomic.Value // *EligibilitySnapshot
	byProvider sync.Map     // provider_type_id -> *providerSnapshot

	updateMu        sync.Mutex // guards coalescing window state
	updateScheduled bool       // true while a coalesced full Update is pending
	lastUpdate      time.Time  // timestamp of last full rebuild

	providerUpdateScheduled map[string]bool      // true while a coalesced per-provider update is pending
	providerLastUpdate      map[string]time.Time // timestamp of last per-provider rebuild
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
		store:                   store,
		providerUpdateScheduled: make(map[string]bool),
		providerLastUpdate:      make(map[string]time.Time),
	}
	e.snapshot.Store(&EligibilitySnapshot{
		Providers:     nil,
		ByPrefix:      make(map[string][]string),
		ByPrefixState: make(map[string][]*ConnectionState),
	})
	return e
}

// Update recomputes the eligibility snapshot from the current store state.
// Connections are ordered by remaining quota (highest first) within each prefix,
// so routing modes such as round_robin and first_eligible prefer healthy
// accounts while still rotating fallback across siblings.
func (e *EligibilityManager) Update(store *Store) {
	next := make(map[string]*providerSnapshot)

	store.RangeByConnID(func(connID string, cs *ConnectionState) bool {
		status := cs.GetStatus()
		// Exclude routing-terminal and non-eligible statuses, plus active cooldowns.
		if status.IsRoutingTerminal() {
			return true
		}
		if status != StatusReady && status != StatusDegraded {
			return true
		}
		if cs.IsInCooldown() {
			return true
		}

		prefix := cs.Prefix
		ps := next[prefix]
		if ps == nil {
			ps = &providerSnapshot{}
			next[prefix] = ps
		}
		ps.States = append(ps.States, cs)
		return true
	})

	for _, ps := range next {
		sortProviderSnapshot(ps)
	}

	for prefix, ps := range next {
		e.byProvider.Store(prefix, ps)
	}

	// Providers that previously had a snapshot but now have no eligible
	// connections must be removed from the aggregate view.
	var stale []string
	e.byProvider.Range(func(key, value any) bool {
		prefix := key.(string)
		if _, ok := next[prefix]; !ok {
			stale = append(stale, prefix)
		}
		return true
	})
	for _, prefix := range stale {
		e.byProvider.Delete(prefix)
	}

	e.rebuildAggregate()
}

// UpdateProvider recomputes the eligibility snapshot for a single provider.
// This avoids scanning and rebuilding unaffected providers during hot-path
// status changes (failover, recovery, cooldown expiry).
func (e *EligibilityManager) UpdateProvider(provider string) {
	ps := &providerSnapshot{}

	if e.store != nil {
		e.store.RangeByConnID(func(connID string, cs *ConnectionState) bool {
			if cs.Prefix != provider {
				return true
			}
			status := cs.GetStatus()
			if status.IsRoutingTerminal() {
				return true
			}
			if status != StatusReady && status != StatusDegraded {
				return true
			}
			if cs.IsInCooldown() {
				return true
			}
			ps.States = append(ps.States, cs)
			return true
		})
	}

	sortProviderSnapshot(ps)
	e.byProvider.Store(provider, ps)
	e.rebuildAggregate()
}

// sortProviderSnapshot orders a provider's eligible connections by remaining
// quota (highest first) and then by recency (least-recently-used first).
func sortProviderSnapshot(ps *providerSnapshot) {
	if ps == nil {
		return
	}
	sort.SliceStable(ps.States, func(i, j int) bool {
		ri, rj := ps.States[i].GetRemainingPct(), ps.States[j].GetRemainingPct()
		if ri != rj {
			return ri > rj
		}
		return ps.States[i].lastUsedAtNano() < ps.States[j].lastUsedAtNano()
	})
	ps.IDs = make([]string, len(ps.States))
	for i, cs := range ps.States {
		ps.IDs[i] = cs.ID
	}
}

// rebuildAggregate builds the full EligibilitySnapshot from per-provider snapshots.
func (e *EligibilityManager) rebuildAggregate() {
	byPrefix := make(map[string][]string)
	byPrefixState := make(map[string][]*ConnectionState)
	var providers []string

	e.byProvider.Range(func(key, value any) bool {
		prefix := key.(string)
		ps := value.(*providerSnapshot)
		if len(ps.States) == 0 {
			return true
		}
		byPrefix[prefix] = ps.IDs
		byPrefixState[prefix] = ps.States
		providers = append(providers, ps.IDs...)
		return true
	})

	e.snapshot.Store(&EligibilitySnapshot{
		Providers:     providers,
		ByPrefix:      byPrefix,
		ByPrefixState: byPrefixState,
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

// ScheduleUpdateProvider coalesces concurrent eligibility rebuild requests for a
// single provider into a single per-provider rebuild per updateCoalesceWindow.
// This is the hot-path entry point: a 429 for one provider only rebuilds that
// provider's snapshot and the aggregate view, leaving all other providers untouched.
func (e *EligibilityManager) ScheduleUpdateProvider(provider string) {
	if provider == "" {
		e.ScheduleUpdate()
		return
	}

	e.updateMu.Lock()
	if e.providerUpdateScheduled[provider] {
		e.updateMu.Unlock()
		return
	}
	if time.Since(e.providerLastUpdate[provider]) >= updateCoalesceWindow {
		e.providerLastUpdate[provider] = time.Now()
		e.updateMu.Unlock()
		e.UpdateProvider(provider)
		return
	}
	e.providerUpdateScheduled[provider] = true
	e.updateMu.Unlock()
	go func() {
		time.Sleep(updateCoalesceWindow)
		e.updateMu.Lock()
		e.providerUpdateScheduled[provider] = false
		e.providerLastUpdate[provider] = time.Now()
		e.updateMu.Unlock()
		e.UpdateProvider(provider)
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
