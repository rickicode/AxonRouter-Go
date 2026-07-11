package connstate

import (
	"sync"
	"time"
)

// Store manages connection states in-memory using sync.Map for high-concurrency access.
type Store struct {
	states sync.Map // map[string]*ConnectionState
}

// NewStore creates a new connection state store.
func NewStore() *Store {
	return &Store{}
}

// Get returns the connection state for a connection ID.
func (s *Store) Get(connID string) *ConnectionState {
	if v, ok := s.states.Load(connID); ok {
		return v.(*ConnectionState)
	}
	return nil
}

// GetOrCreate returns the connection state, creating it if not exists.
func (s *Store) GetOrCreate(connID string) *ConnectionState {
	if v, ok := s.states.Load(connID); ok {
		return v.(*ConnectionState)
	}
	cs := &ConnectionState{
		ID:     connID,
		Status: StatusUnknown,
	}
	actual, _ := s.states.LoadOrStore(connID, cs)
	return actual.(*ConnectionState)
}

// GetByPrefix returns the connection state for a provider by prefix.
func (s *Store) GetByPrefix(prefix string) *ConnectionState {
	var found *ConnectionState
	s.states.Range(func(key, value any) bool {
		cs := value.(*ConnectionState)
		if cs.Prefix == prefix {
			found = cs
			return false
		}
		return true
	})
	return found
}

// Set creates or updates a connection state entry.
func (s *Store) Set(connID string, cs *ConnectionState) {
	s.states.Store(connID, cs)
}

// Delete removes a connection from the store.
func (s *Store) Delete(connID string) {
	s.states.Delete(connID)
}

// RecordSuccess records a successful request.
func (s *Store) RecordSuccess(connID string) {
	cs := s.GetOrCreate(connID)
	cs.SetStatus(StatusReady, "")
}

// RecordFailure records a failed request with error detection.
func (s *Store) RecordFailure(connID string, det ErrorDetection) {
	cs := s.GetOrCreate(connID)

	if det.Scope == "model" {
		if det.ModelID != "" {
			// Model-level cooldown
			if det.CooldownUntil != nil {
				cs.SetModelCooldown(det.ModelID, *det.CooldownUntil)
			}
		} else {
			// ponytail: model-scoped error without ModelID — fall through to connection-level.
			// Callers MUST pass modelID to DetectError to avoid this path.
			if det.Category == ErrorRateLimit && det.CooldownUntil != nil {
				cs.SetCooldown(*det.CooldownUntil)
			}
		}
	} else {
		// Connection-level status change
		switch {
		case det.Category == ErrorQuota && det.CooldownUntil != nil:
			cs.SetQuotaCooldown(*det.CooldownUntil)
		case det.CooldownUntil != nil:
			cs.SetCooldown(*det.CooldownUntil)
		case det.Category == ErrorRateLimit:
			// Rate limit without explicit CooldownUntil: use default short cooldown
			cs.SetCooldown(time.Now().Add(60 * time.Second))
		default:
			cs.SetStatus(det.Status, det.Message)
		}
	}
}

// UpdateStatus updates the status of a connection.
func (s *Store) UpdateStatus(connID string, status Status) {
	cs := s.GetOrCreate(connID)
	cs.SetStatus(status, "")
}

// UpdateCooldown sets a cooldown for a connection.
func (s *Store) UpdateCooldown(connID string, until time.Time) {
	cs := s.GetOrCreate(connID)
	cs.SetCooldown(until)
}

// Range iterates over all connection states.
func (s *Store) Range(fn func(connID string, cs *ConnectionState) bool) {
	s.states.Range(func(key, value any) bool {
		return fn(key.(string), value.(*ConnectionState))
	})
}

// RangeByConnID iterates over all connection states by connection ID.
func (s *Store) RangeByConnID(fn func(connID string, cs *ConnectionState) bool) {
	s.states.Range(func(key, value any) bool {
		return fn(key.(string), value.(*ConnectionState))
	})
}

// Snapshot returns a slice of all connection state snapshots.
func (s *Store) Snapshot() []ConnectionStateSnapshot {
	var snapshots []ConnectionStateSnapshot
	s.states.Range(func(_, value any) bool {
		cs := value.(*ConnectionState)
		snapshots = append(snapshots, cs.Snapshot())
		return true
	})
	return snapshots
}

// HealthyCount returns the number of healthy connections.
func (s *Store) HealthyCount() int {
	count := 0
	s.states.Range(func(_, value any) bool {
		cs := value.(*ConnectionState)
		if cs.GetStatus() == StatusReady {
			count++
		}
		return true
	})
	return count
}

// SeedConnection creates or updates a connection state entry from DB data.
// Used to keep the in-memory store in sync with the database.
// Builds a fully-initialized ConnectionState before publishing to map to avoid partial-read races.
func (s *Store) SeedConnection(connID, prefix, status string, priority int) {
	// Build status first so the struct is complete before anyone can see it.
	var st Status
	switch status {
	case "ready":
		st = StatusReady
	case "rate_limited":
		st = StatusRateLimited
	case "quota_exhausted":
		st = StatusQuotaExhausted
	case "balance_empty":
		st = StatusBalanceEmpty
	case "auth_failed":
		st = StatusAuthFailed
	case "suspended":
		st = StatusSuspended
	case "disabled":
		st = StatusDisabled
	case "degraded":
		st = StatusDegraded
	default:
		st = StatusUnknown
	}

	// Try atomic insert with a fully-initialized struct.
	full := &ConnectionState{
		ID:       connID,
		Prefix:   prefix,
		Priority: priority,
		Status:   st,
	}
	actual, loaded := s.states.LoadOrStore(connID, full)
	if loaded {
		// Already exists — update under lock.
		cs := actual.(*ConnectionState)
		cs.mu.Lock()
		cs.Prefix = prefix
		cs.Priority = priority
		cs.Status = st
		cs.mu.Unlock()
	}
}

// All returns all connection states as a slice.
func (s *Store) All() []*ConnectionState {
	var all []*ConnectionState
	s.states.Range(func(_, value any) bool {
		all = append(all, value.(*ConnectionState))
		return true
	})
	return all
}

// RangeActive iterates over all connection states and collects active ones.
func (s *Store) RangeActive() map[string]bool {
	active := make(map[string]bool)
	s.states.Range(func(_, value any) bool {
		cs := value.(*ConnectionState)
		active[cs.ID] = true
		return true
	})
	return active
}
