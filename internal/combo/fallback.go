package combo

import (
	"sync"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
)

// FallbackManager tracks circuit breakers per connection and determines
// if a connection can be used or should be skipped.
type FallbackManager struct {
	mu       sync.RWMutex
	breakers map[string]*connstate.CircuitBreaker // connID → CircuitBreaker
}

// NewFallbackManager creates a new fallback manager.
func NewFallbackManager() *FallbackManager {
	return &FallbackManager{
		breakers: make(map[string]*connstate.CircuitBreaker),
	}
}

// GetBreaker returns the circuit breaker for a connection, creating one if needed.
func (fm *FallbackManager) GetBreaker(connID string) *connstate.CircuitBreaker {
	fm.mu.RLock()
	cb, ok := fm.breakers[connID]
	fm.mu.RUnlock()
	if ok {
		return cb
	}

	fm.mu.Lock()
	defer fm.mu.Unlock()
	if cb, ok = fm.breakers[connID]; ok {
		return cb
	}
	cb = connstate.NewCircuitBreaker(connstate.DefaultCircuitBreakerConfig())
	fm.breakers[connID] = cb
	return cb
}

// CanUseConnection checks both circuit breaker and connection state.
func (fm *FallbackManager) CanUseConnection(cs *connstate.ConnectionState) bool {
	status := cs.GetStatus()
	if !status.IsEligible() {
		return false
	}
	cb := fm.GetBreaker(cs.ID)
	return cb.IsAllowed()
}

// RecordSuccess records a success for the connection's circuit breaker.
func (fm *FallbackManager) RecordSuccess(connID string) {
	fm.GetBreaker(connID).RecordSuccess()
}

// RecordFailure records a failure for the connection's circuit breaker.
func (fm *FallbackManager) RecordFailure(connID string) {
	fm.GetBreaker(connID).RecordFailure()
}

// Cleanup removes circuit breakers for connections no longer in the store.
func (fm *FallbackManager) Cleanup(activeConnIDs map[string]bool) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	for id := range fm.breakers {
		if !activeConnIDs[id] {
			delete(fm.breakers, id)
		}
	}
}
