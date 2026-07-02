package quota

import (
	"sync"
	"time"
)

// exhaustionEntry tracks a 429-sourced exhaustion mark with a TTL.
type exhaustionEntry struct {
	markedAt time.Time
	expiresAt time.Time
}

// ExhaustionCache is an in-memory cache tracking connections marked as
// quota-exhausted from 429 responses. Entries auto-expire after their TTL.
// Matches OmniRoute's markAccountExhaustedFrom429 / isAccountQuotaExhausted.
type ExhaustionCache struct {
	mu      sync.RWMutex
	entries map[string]exhaustionEntry
}

// NewExhaustionCache creates a new exhaustion cache.
func NewExhaustionCache() *ExhaustionCache {
	return &ExhaustionCache{
		entries: make(map[string]exhaustionEntry),
	}
}

// MarkExhausted marks a connection as quota-exhausted for the given TTL.
// Called when a 429 is received from an upstream provider.
func (ec *ExhaustionCache) MarkExhausted(connID string, ttl time.Duration) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	now := time.Now()
	ec.entries[connID] = exhaustionEntry{
		markedAt:  now,
		expiresAt: now.Add(ttl),
	}
}

// IsExhausted returns true if the connection is currently marked as exhausted
// and the TTL has not expired. Returns false if not marked or expired.
func (ec *ExhaustionCache) IsExhausted(connID string) bool {
	ec.mu.RLock()
	entry, ok := ec.entries[connID]
	ec.mu.RUnlock()
	if !ok {
		return false
	}
	return time.Now().Before(entry.expiresAt)
}

// Clear removes the exhaustion mark for a connection (e.g. after successful
// quota fetch confirms the connection is no longer exhausted).
func (ec *ExhaustionCache) Clear(connID string) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	delete(ec.entries, connID)
}

// Cleanup removes expired entries. Should be called periodically.
func (ec *ExhaustionCache) Cleanup() {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	now := time.Now()
	for k, v := range ec.entries {
		if now.After(v.expiresAt) {
			delete(ec.entries, k)
		}
	}
}

// Default exhaustion TTL: 5 minutes (matches OmniRoute EXHAUSTED_TTL_MS).
const DefaultExhaustionTTL = 5 * time.Minute
