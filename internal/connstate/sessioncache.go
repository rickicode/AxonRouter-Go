package connstate

import (
	"sync"
	"time"
)

// SessionCache maps a composite provider+session+model key to a connection
// ID so repeat calls from the same session prefer the same connection. Cached
// entries expire after a configured TTL and are evicted by a background
// cleanup loop and lazily on access.
type SessionCache struct {
	mu      sync.RWMutex
	entries map[string]sessionEntry
	ttl     time.Duration
	stop    chan struct{}
}

type sessionEntry struct {
	connID string
	expiry time.Time
}

// NewSessionCache creates a session cache with the default one-hour TTL.
func NewSessionCache() *SessionCache {
	return NewSessionCacheWithTTL(time.Hour)
}

// NewSessionCacheWithTTL creates a session cache with a custom TTL. A
// background goroutine removes expired entries every TTL/2.
func NewSessionCacheWithTTL(ttl time.Duration) *SessionCache {
	if ttl <= 0 {
		ttl = time.Hour
	}
	sc := &SessionCache{
		entries: make(map[string]sessionEntry),
		ttl:     ttl,
		stop:    make(chan struct{}),
	}
	go sc.cleanupLoop(ttl / 2)
	return sc
}

func (sc *SessionCache) cleanupLoop(interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			sc.evictExpired()
		case <-sc.stop:
			return
		}
	}
}

func (sc *SessionCache) evictExpired() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	now := time.Now()
	for k, e := range sc.entries {
		if now.After(e.expiry) {
			delete(sc.entries, k)
		}
	}
}

// Stop halts the background cleanup goroutine. Stop should be called only
// once per cache; subsequent calls panic.
func (sc *SessionCache) Stop() {
	close(sc.stop)
}

// Get returns the connection ID for a key, or false if the key is missing
// or has expired. Expired entries are deleted lazily.
func (sc *SessionCache) Get(key string) (string, bool) {
	sc.mu.RLock()
	e, ok := sc.entries[key]
	sc.mu.RUnlock()
	if !ok {
		return "", false
	}
	if time.Now().After(e.expiry) {
		sc.mu.Lock()
		if ent, ok := sc.entries[key]; ok && time.Now().After(ent.expiry) {
			delete(sc.entries, key)
		}
		sc.mu.Unlock()
		return "", false
	}
	return e.connID, true
}

// Put stores a connection ID under key with the configured TTL.
func (sc *SessionCache) Put(key, connID string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.entries[key] = sessionEntry{
		connID: connID,
		expiry: time.Now().Add(sc.ttl),
	}
}

// SessionKey returns the composite key used by SessionCache.
func SessionKey(provider, sessionID, modelID string) string {
	return provider + "::" + sessionID + "::" + modelID
}
