package quota

import (
	"strings"
	"sync"
	"time"
)

// exhaustionEntry tracks a 429-sourced exhaustion mark with a TTL.
type exhaustionEntry struct {
	markedAt  time.Time
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

// ExhaustKey builds a composite key for per-model or per-connection exhaustion.
// scope may be empty for the connection-wide key.
func ExhaustKey(connID, scope string) string {
	if scope == "" {
		return connID
	}
	return connID + "\x00" + scope
}

// MarkExhausted marks a key as quota-exhausted for the given TTL.
// Called when a 429 is received from an upstream provider.
// For per-model scope use MarkExhausted(ExhaustKey(connID, modelScope), ttl).
func (ec *ExhaustionCache) MarkExhausted(key string, ttl time.Duration) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	now := time.Now()
	ec.entries[key] = exhaustionEntry{
		markedAt:  now,
		expiresAt: now.Add(ttl),
	}
}

// IsExhausted returns true if the given key is currently marked as exhausted
// and the TTL has not expired. Returns false if not marked or expired.
func (ec *ExhaustionCache) IsExhausted(key string) bool {
	return ec.IsExhaustedAt(key, time.Now())
}

// IsExhaustedAt returns true if the given key is marked as exhausted and the
// TTL has not expired at the provided time.
func (ec *ExhaustionCache) IsExhaustedAt(key string, now time.Time) bool {
	ec.mu.RLock()
	entry, ok := ec.entries[key]
	ec.mu.RUnlock()
	if !ok {
		return false
	}
	return now.Before(entry.expiresAt)
}

// IsExhaustedScope is a convenience for checking a connID + scope composite key.
func (ec *ExhaustionCache) IsExhaustedScope(connID, scope string) bool {
	return ec.IsExhausted(ExhaustKey(connID, scope))
}

// IsExhaustedScopeAt is a convenience for checking a connID + scope composite
// key at the provided time.
func (ec *ExhaustionCache) IsExhaustedScopeAt(connID, scope string, now time.Time) bool {
	return ec.IsExhaustedAt(ExhaustKey(connID, scope), now)
}

// Clear removes the exhaustion mark for a key (e.g. after successful
// quota fetch confirms the connection is no longer exhausted).
func (ec *ExhaustionCache) Clear(key string) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	delete(ec.entries, key)
}

// ScopesForConn returns the non-empty scopes currently marked exhausted for a
// given connection ID. Used by the model prober to recover per-model locks.
func (ec *ExhaustionCache) ScopesForConn(connID string) []string {
	ec.mu.RLock()
	defer ec.mu.RUnlock()
	prefix := connID + "\x00"
	scopes := make([]string, 0)
	for k := range ec.entries {
		if strings.HasPrefix(k, prefix) {
			if now := time.Now(); now.Before(ec.entries[k].expiresAt) {
				scopes = append(scopes, k[len(prefix):])
			}
		}
	}
	return scopes
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

// TTLFromCooldown returns the time-to-live for an exhaustion mark derived from
// a detection result. When CooldownUntil is present (rate-limit or quota cooldown),
// the exhaustion mirror expires at the same time so cooldown and exhaustion stay
// consistent. Falls back to defaultTTL when no cooldown is set.
func TTLFromCooldown(cooldownUntil *time.Time, defaultTTL time.Duration) time.Duration {
	if cooldownUntil == nil {
		return defaultTTL
	}
	ttl := time.Until(*cooldownUntil)
	if ttl <= 0 {
		return defaultTTL
	}
	return ttl
}
