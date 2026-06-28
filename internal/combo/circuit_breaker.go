package combo

import (
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
)

// CircuitBreaker implements the CLOSED → OPEN → HALF_OPEN state machine per connection.
type CircuitBreaker struct {
	mu           sync.Mutex
	State        connstate.CircuitState
	FailureCount int
	SuccessCount int
	LastFailure  time.Time
	OpenedAt     time.Time
}

// NewCircuitBreaker creates a circuit breaker in CLOSED state.
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{State: connstate.CBClosed}
}

// CanExecute returns true if requests are allowed through.
func (cb *CircuitBreaker) CanExecute() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.State {
	case connstate.CBClosed:
		return true
	case connstate.CBOpen:
		if time.Since(cb.OpenedAt) > 60*time.Second {
			cb.State = connstate.CBHalfOpen
			return true
		}
		return false
	case connstate.CBHalfOpen:
		return true
	}
	return true
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.State == connstate.CBHalfOpen {
		cb.SuccessCount++
		if cb.SuccessCount >= 2 {
			cb.State = connstate.CBClosed
			cb.FailureCount = 0
			cb.SuccessCount = 0
		}
	}
}

// RecordFailure records a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.FailureCount++
	cb.LastFailure = time.Now()

	if cb.State == connstate.CBHalfOpen {
		cb.State = connstate.CBOpen
		cb.OpenedAt = time.Now()
	} else if cb.FailureCount >= 3 {
		cb.State = connstate.CBOpen
		cb.OpenedAt = time.Now()
	}
}

// Reset forces the circuit breaker back to CLOSED.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.State = connstate.CBClosed
	cb.FailureCount = 0
	cb.SuccessCount = 0
}
