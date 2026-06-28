package connstate

import (
	"sync"
	"time"
)

// CircuitBreakerConfig holds configuration for a circuit breaker.
type CircuitBreakerConfig struct {
	FailureThreshold int           // Number of failures before opening
	SuccessThreshold int           // Number of successes before closing from half-open
	ResetTimeout     time.Duration // Time to wait before transitioning from open to half-open
}

// DefaultCircuitBreakerConfig returns PRD-correct defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		ResetTimeout:     60 * time.Second,
	}
}

// CircuitBreaker implements the CLOSED→OPEN→HALF_OPEN state machine.
type CircuitBreaker struct {
	config       CircuitBreakerConfig
	state        CircuitState
	failCount    int
	successCount int
	lastFailure  time.Time
	mu           sync.Mutex
}

// NewCircuitBreaker creates a new circuit breaker with the given config.
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		config: config,
		state:  CBClosed,
	}
}

// GetState returns the current circuit breaker state.
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CBOpen && time.Since(cb.lastFailure) >= cb.config.ResetTimeout {
		cb.state = CBHalfOpen
		cb.successCount = 0
	}
	return cb.state
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CBClosed:
		cb.failCount = 0
	case CBHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.config.SuccessThreshold {
			cb.state = CBClosed
			cb.failCount = 0
			cb.successCount = 0
		}
	case CBOpen:
		// Ignore success while open
	}
}

// RecordFailure records a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailure = time.Now()

	switch cb.state {
	case CBClosed:
		cb.failCount++
		if cb.failCount >= cb.config.FailureThreshold {
			cb.state = CBOpen
		}
	case CBHalfOpen:
		cb.state = CBOpen
		cb.successCount = 0
	case CBOpen:
		// Already open, keep open
	}
}

// IsAllowed returns true if requests should be sent to this provider.
func (cb *CircuitBreaker) IsAllowed() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CBClosed {
		return true
	}
	if cb.state == CBOpen && time.Since(cb.lastFailure) >= cb.config.ResetTimeout {
		cb.state = CBHalfOpen
		cb.successCount = 0
		return true
	}
	return cb.state == CBHalfOpen
}

// Reset forces the circuit breaker back to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = CBClosed
	cb.failCount = 0
	cb.successCount = 0
}
