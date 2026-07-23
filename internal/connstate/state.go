package connstate

import (
	"sync"
	"sync/atomic"
	"time"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState string

const (
	CBClosed   CircuitState = "closed"
	CBOpen     CircuitState = "open"
	CBHalfOpen CircuitState = "half_open"
)

// Status represents the current status of a provider connection.
// Aligned to DB vocabulary (migrations.go:30).
type Status string

const (
	StatusUnknown        Status = "unknown"
	StatusReady          Status = "ready"
	StatusRateLimited    Status = "rate_limited"
	StatusQuotaExhausted Status = "quota_exhausted"
	StatusDisabled       Status = "disabled"
	StatusDegraded       Status = "degraded"
	StatusCooldown       Status = "cooldown"
)

// IsEligible returns true if the status indicates the connection can be used.
func (s Status) IsEligible() bool {
	return s == StatusReady || s == StatusDegraded
}

// IsHealable returns true for transient, non-terminal statuses that can be
// reset to ready when a cooldown expires or a request succeeds.
func (s Status) IsHealable() bool {
	switch s {
	case StatusCooldown, StatusRateLimited, StatusQuotaExhausted, StatusDegraded:
		return true
	}
	return false
}

// IsRoutingTerminal returns true if the status means the connection should not
// be selected for routing regardless of cooldown state. This guards against stale
// eligibility snapshots re-picking an account that was just marked failed.
func (s Status) IsRoutingTerminal() bool {
	return s == StatusDisabled
}

// ConnectionState holds the live state of a single provider connection.
type ConnectionState struct {
	ID            string
	ProviderID    int64
	Prefix        string
	Priority      int // Higher = tried first
	Status        Status
	LastCheckAt   time.Time
	LastError      string
	DisabledReason string // reason when Status == StatusDisabled (auth_failed, balance_empty, manual, ...)
	ResponseTime   time.Duration
	FailCount      int
	BanCount       int // Consecutive ban signals (auth/quota/balance)
	SuccessCount   int
	CooldownUntil  *time.Time
	RemainingPct   float64 // cached min remaining quota percentage (0-100)
	ModelLimits   sync.Map // modelID -> *ModelLimitState
	mu            sync.RWMutex
	lastUsedAt    atomic.Int64
}

// GetStatus returns the current status (thread-safe).
func (cs *ConnectionState) GetStatus() Status {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.Status
}

// GetPriority returns the priority (thread-safe).
func (cs *ConnectionState) GetPriority() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.Priority
}

// GetBanCount returns the ban count (thread-safe).
func (cs *ConnectionState) GetBanCount() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.BanCount
}

// ResetBanCount clears the ban count to 0 (thread-safe).
func (cs *ConnectionState) ResetBanCount() {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.BanCount = 0
}

// GetRemainingPct returns the cached minimum remaining quota percentage.
func (cs *ConnectionState) GetRemainingPct() float64 {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.RemainingPct
}

// SetRemainingPct stores the minimum remaining quota percentage (thread-safe).
func (cs *ConnectionState) SetRemainingPct(pct float64) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.RemainingPct = pct
}

// RecordUsed marks this connection as having just been selected. In-memory only.
func (cs *ConnectionState) RecordUsed() {
	cs.lastUsedAt.Store(time.Now().UnixNano())
}

// LastUsedAt returns the timestamp of the last selection (zero if never used).
func (cs *ConnectionState) LastUsedAt() time.Time {
	return time.Unix(0, cs.lastUsedAt.Load())
}

// lastUsedAtNano returns the raw nanosecond timestamp for cheap comparisons.
func (cs *ConnectionState) lastUsedAtNano() int64 {
	return cs.lastUsedAt.Load()
}

// SetStatus updates the status and timestamps (thread-safe).
func (cs *ConnectionState) SetStatus(status Status, err string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.Status = status
	cs.LastCheckAt = time.Now()
	cs.LastError = err
	if status == StatusReady {
		cs.SuccessCount++
		cs.FailCount = 0
		cs.BanCount = 0 // reset ban count on success
		cs.CooldownUntil = nil
	} else if status == StatusDisabled || status == StatusQuotaExhausted {
		cs.FailCount++
		cs.BanCount++
		if status == StatusDisabled {
			cs.DisabledReason = err
		}
	}
}

// SetCooldown sets a cooldown timer.
func (cs *ConnectionState) SetCooldown(until time.Time) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.CooldownUntil = &until
	cs.Status = StatusCooldown
}

// SetQuotaCooldown sets a quota-exhausted cooldown (midnight UTC recovery).
// Unlike SetCooldown, it preserves StatusQuotaExhausted so the DB recovery
// path in QuotaScheduler recognises it correctly.
func (cs *ConnectionState) SetQuotaCooldown(until time.Time) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.CooldownUntil = &until
	cs.Status = StatusQuotaExhausted
	cs.FailCount++
	cs.BanCount++
}

// IsInCooldown checks if the connection is in cooldown.
func (cs *ConnectionState) IsInCooldown() bool {
	return cs.IsInCooldownAt(time.Now())
}

// IsInCooldownAt checks if the connection is in cooldown at the given time.
func (cs *ConnectionState) IsInCooldownAt(now time.Time) bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	if cs.CooldownUntil == nil {
		return false
	}
	return now.Before(*cs.CooldownUntil)
}

// IsCooldownExpired checks if the connection has an expired cooldown timer (thread-safe).
func (cs *ConnectionState) IsCooldownExpired() bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.CooldownUntil != nil && time.Now().After(*cs.CooldownUntil)
}

// SetResponseTime updates the response time metric (thread-safe).
func (cs *ConnectionState) SetResponseTime(d time.Duration) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.ResponseTime = d
}

// Snapshot returns a copy of the connection state for external use.
func (cs *ConnectionState) Snapshot() ConnectionStateSnapshot {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return ConnectionStateSnapshot{
		ID:           cs.ID,
		ProviderID:   cs.ProviderID,
		Prefix:       cs.Prefix,
		Status:       cs.Status,
		LastCheckAt:  cs.LastCheckAt,
		LastError:    cs.LastError,
		ResponseTime: cs.ResponseTime,
		FailCount:    cs.FailCount,
		SuccessCount: cs.SuccessCount,
	}
}

// GetModelLimit returns the model limit state, creating if needed.
func (cs *ConnectionState) GetModelLimit(modelID string) *ModelLimitState {
	if v, ok := cs.ModelLimits.Load(modelID); ok {
		return v.(*ModelLimitState)
	}
	mls := &ModelLimitState{ModelID: modelID}
	actual, _ := cs.ModelLimits.LoadOrStore(modelID, mls)
	return actual.(*ModelLimitState)
}

// IsModelInCooldown checks if a specific model is in cooldown.
func (cs *ConnectionState) IsModelInCooldown(modelID string) bool {
	return cs.IsModelInCooldownAt(modelID, time.Now())
}

// IsModelInCooldownAt checks if a specific model is in cooldown at the given time.
func (cs *ConnectionState) IsModelInCooldownAt(modelID string, now time.Time) bool {
	mls := cs.GetModelLimit(modelID)
	return mls.IsInCooldownAt(now)
}

// SetModelCooldown sets a cooldown for a specific model.
func (cs *ConnectionState) SetModelCooldown(modelID string, until time.Time) {
	mls := cs.GetModelLimit(modelID)
	mls.SetCooldown(until)
}

// ModelCooldowns returns a snapshot of all models currently in cooldown.
// Used by the model prober to test locked models after proxy/IP rotation.
func (cs *ConnectionState) ModelCooldowns() map[string]time.Time {
	m := make(map[string]time.Time)
	cs.ModelLimits.Range(func(key, value any) bool {
		modelID := key.(string)
		mls := value.(*ModelLimitState)
		mls.mu.RLock()
		until := mls.CooldownUntil
		mls.mu.RUnlock()
		if until != nil && time.Now().Before(*until) {
			m[modelID] = *until
		}
		return true
	})
	return m
}

// ClearModelCooldown clears the cooldown for a specific model.
func (cs *ConnectionState) ClearModelCooldown(modelID string) {
	mls := cs.GetModelLimit(modelID)
	mls.ClearCooldown()
}

// ConnectionStateSnapshot is an immutable copy of connection state.
type ConnectionStateSnapshot struct {
	ID           string
	ProviderID   int64
	Prefix       string
	Status       Status
	LastCheckAt  time.Time
	LastError    string
	ResponseTime time.Duration
	FailCount    int
	SuccessCount int
}

// ModelLimitState tracks rate limit state for a specific model on a connection.
type ModelLimitState struct {
	ModelID       string
	CooldownUntil *time.Time
	TPMRemaining  int64
	TPMLimit      int64
	RPMRemaining  int64
	RPMLimit      int64
	mu            sync.RWMutex
}

// IsInCooldown checks if the model is in cooldown.
func (mls *ModelLimitState) IsInCooldown() bool {
	return mls.IsInCooldownAt(time.Now())
}

// IsInCooldownAt checks if the model is in cooldown at the given time.
func (mls *ModelLimitState) IsInCooldownAt(now time.Time) bool {
	mls.mu.RLock()
	defer mls.mu.RUnlock()
	if mls.CooldownUntil == nil {
		return false
	}
	return now.Before(*mls.CooldownUntil)
}

// SetCooldown sets a cooldown timer for this model.
func (mls *ModelLimitState) SetCooldown(until time.Time) {
	mls.mu.Lock()
	defer mls.mu.Unlock()
	mls.CooldownUntil = &until
}

// ClearCooldown clears the cooldown timer.
func (mls *ModelLimitState) ClearCooldown() {
	mls.mu.Lock()
	defer mls.mu.Unlock()
	mls.CooldownUntil = nil
}

// SetTPMRemaining updates the TPM remaining count.
func (mls *ModelLimitState) SetTPMRemaining(n int64) {
	mls.mu.Lock()
	defer mls.mu.Unlock()
	mls.TPMRemaining = n
}

// SetRPMRemaining updates the RPM remaining count.
func (mls *ModelLimitState) SetRPMRemaining(n int64) {
	mls.mu.Lock()
	defer mls.mu.Unlock()
	mls.RPMRemaining = n
}
