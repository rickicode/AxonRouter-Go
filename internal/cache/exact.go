package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// ExactCache provides an in-memory exact-match response cache.
type ExactCache struct {
	mu         sync.RWMutex
	data       map[string]CacheEntry
	maxEntries int
	hits       atomic.Uint64
	misses     atomic.Uint64
}

// NewExactCache creates a bounded exact-match cache.
func NewExactCache(maxEntries int) *ExactCache {
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	return &ExactCache{
		data:       make(map[string]CacheEntry),
		maxEntries: maxEntries,
	}
}

// ComputeKey builds a cache key from request body and model.
func ComputeKey(body []byte, model string) string {
	h := sha256.New()
	h.Write(body)
	h.Write([]byte(model))
	h.Write([]byte("false")) // stream flag for exact cache (only non-stream)
	return hex.EncodeToString(h.Sum(nil))
}

// IsStreamRequest detects whether body asks for streaming.
func IsStreamRequest(body []byte) bool {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return false
	}
	v, ok := m["stream"]
	if !ok {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		b, _ := strconv.ParseBool(val)
		return b
	}
	return false
}

// Get retrieves a cached entry.
func (c *ExactCache) Get(key string) (CacheEntry, bool) {
	c.mu.RLock()
	entry, ok := c.data[key]
	c.mu.RUnlock()
	if ok {
		c.hits.Add(1)
		return entry, true
	}
	c.misses.Add(1)
	return CacheEntry{}, false
}

// Set stores a response in the cache.
func (c *ExactCache) Set(key string, entry CacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.data) >= c.maxEntries {
		// Simple random eviction: delete the first key we iterate over.
		for k := range c.data {
			delete(c.data, k)
			break
		}
	}
	entry.CreatedAt = time.Now()
	c.data[key] = entry
}

// Flush clears all cached entries.
func (c *ExactCache) Flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string]CacheEntry)
	c.hits.Store(0)
	c.misses.Store(0)
}

// Stats returns current cache statistics.
func (c *ExactCache) Stats() CacheStats {
	c.mu.RLock()
	size := len(c.data)
	c.mu.RUnlock()
	return CacheStats{
		Hits:   c.hits.Load(),
		Misses: c.misses.Load(),
		Size:   size,
	}
}
