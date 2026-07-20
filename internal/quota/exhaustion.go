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
	entries sync.Map // key: string, value: exhaustionEntry
}

// NewExhaustionCache creates a new exhaustion cache.
func NewExhaustionCache() *ExhaustionCache {
	return &ExhaustionCache{}
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
	now := time.Now()
	ec.entries.Store(key, exhaustionEntry{
		markedAt:  now,
		expiresAt: now.Add(ttl),
	})
}

// IsExhausted returns true if the given key is currently marked as exhausted
// and the TTL has not expired. Returns false if not marked or expired.
func (ec *ExhaustionCache) IsExhausted(key string) bool {
	value, ok := ec.entries.Load(key)
	if !ok {
		return false
	}
	entry := value.(exhaustionEntry)
	return time.Now().Before(entry.expiresAt)
}

// IsExhaustedScope is a convenience for checking a connID + scope composite key.
func (ec *ExhaustionCache) IsExhaustedScope(connID, scope string) bool {
	return ec.IsExhausted(ExhaustKey(connID, scope))
}

// Clear removes the exhaustion mark for a key (e.g. after successful
// quota fetch confirms the connection is no longer exhausted).
func (ec *ExhaustionCache) Clear(key string) {
	ec.entries.Delete(key)
}

// ScopesForConn returns the non-empty scopes currently marked exhausted for a
// given connection ID. Used by the model prober to recover per-model locks.
func (ec *ExhaustionCache) ScopesForConn(connID string) []string {
	prefix := connID + "\x00"
	scopes := make([]string, 0)
	ec.entries.Range(func(k, v any) bool {
		key := k.(string)
		if strings.HasPrefix(key, prefix) {
			entry := v.(exhaustionEntry)
			if time.Now().Before(entry.expiresAt) {
				scopes = append(scopes, key[len(prefix):])
			}
		}
		return true
	})
	return scopes
}

// Cleanup removes expired entries. Should be called periodically.
func (ec *ExhaustionCache) Cleanup() {
	now := time.Now()
	ec.entries.Range(func(k, v any) bool {
		entry := v.(exhaustionEntry)
		if now.After(entry.expiresAt) {
			ec.entries.Delete(k)
		}
		return true
	})
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
