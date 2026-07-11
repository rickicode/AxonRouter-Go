package cache

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// defaultCacheTTL is used when a persistent cache is created without an explicit TTL.
const defaultCacheTTL = 5 * time.Minute

// ExactCache provides an in-memory exact-match response cache with optional
// SQLite persistence and TTL-based expiry.
type ExactCache struct {
	mu         sync.RWMutex
	data       map[string]CacheEntry
	maxEntries int
	ttl        atomic.Int64 // nanoseconds; 0 means no expiry
	hits       atomic.Uint64
	misses     atomic.Uint64
	db         *sql.DB
}

// NewExactCache creates a bounded in-memory exact-match cache with no expiry.
func NewExactCache(maxEntries int) *ExactCache {
	return newExactCache(maxEntries, 0, nil)
}

// NewExactCacheWithTTL creates a bounded in-memory exact-match cache with TTL expiry.
func NewExactCacheWithTTL(maxEntries int, ttl time.Duration) *ExactCache {
	return newExactCache(maxEntries, ttl, nil)
}

// NewPersistentCache creates an exact-match cache backed by SQLite. Existing
// non-expired entries are loaded into memory on creation.
func NewPersistentCache(db *sql.DB, maxEntries int, ttl time.Duration) *ExactCache {
	c := newExactCache(maxEntries, ttl, db)
	c.loadFromDB()
	return c
}

func newExactCache(maxEntries int, ttl time.Duration, db *sql.DB) *ExactCache {
	if maxEntries <= 0 {
		maxEntries = 1000
	}
	if ttl <= 0 && db != nil {
		ttl = defaultCacheTTL
	}
	c := &ExactCache{
		data:       make(map[string]CacheEntry),
		maxEntries: maxEntries,
		db:         db,
	}
	c.ttl.Store(ttl.Nanoseconds())
	return c
}

// SetTTL updates the TTL used for future writes and lazy expiry checks.
func (c *ExactCache) SetTTL(ttl time.Duration) {
	c.ttl.Store(ttl.Nanoseconds())
}

func (c *ExactCache) ttlDuration() time.Duration {
	return time.Duration(c.ttl.Load())
}

// ComputeKey builds a cache key from request body and model. The body is
// normalized to canonical JSON (sorted object keys) so semantically identical
// requests with different key ordering share the same key.
func ComputeKey(body []byte, model string) string {
	canonical := canonicalJSON(body)
	h := sha256.New()
	h.Write(canonical)
	h.Write([]byte(model))
	h.Write([]byte("false")) // stream flag for exact cache (only non-stream)
	return hex.EncodeToString(h.Sum(nil))
}

// canonicalJSON re-marshals JSON with sorted object keys. On any error it
// returns the original bytes.
func canonicalJSON(body []byte) []byte {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return body
	}
	out, err := json.Marshal(v)
	if err != nil {
		return body
	}
	return out
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

// Get retrieves a cached entry. Expired entries are removed lazily.
func (c *ExactCache) Get(key string) (CacheEntry, bool) {
	c.mu.RLock()
	entry, ok := c.data[key]
	c.mu.RUnlock()

	if ok {
		if c.isExpired(entry) {
			c.mu.Lock()
			delete(c.data, key)
			c.mu.Unlock()
			c.misses.Add(1)
			return CacheEntry{}, false
		}
		c.hits.Add(1)
		return entry, true
	}

	if c.db == nil {
		c.misses.Add(1)
		return CacheEntry{}, false
	}

	// Fallback to persistent store.
	var body, contentType string
	var statusCode, createdAt, expiresAt int64
	err := c.db.QueryRow(`
		SELECT body, status_code, content_type, created_at, expires_at
		FROM response_cache
		WHERE hash = ?
	`, key).Scan(&body, &statusCode, &contentType, &createdAt, &expiresAt)
	if err != nil || expiresAt <= time.Now().Unix() {
		c.misses.Add(1)
		return CacheEntry{}, false
	}

	entry = CacheEntry{
		Body:        []byte(body),
		StatusCode:  int(statusCode),
		ContentType: contentType,
		CreatedAt:   time.Unix(createdAt, 0),
	}

	c.mu.Lock()
	c.ensureCapacity()
	c.data[key] = entry
	c.mu.Unlock()

	c.hits.Add(1)
	return entry, true
}

// Set stores a response in the cache. It also persists to SQLite when a DB is attached.
func (c *ExactCache) Set(key string, entry CacheEntry) {
	now := time.Now()
	entry.CreatedAt = now

	c.mu.Lock()
	c.ensureCapacity()
	c.data[key] = entry
	c.mu.Unlock()

	if c.db != nil {
		ttl := c.ttlDuration()
		expires := now.Add(ttl)
		if ttl <= 0 {
			expires = now.Add(defaultCacheTTL)
		}
		_, _ = c.db.Exec(`
			INSERT INTO response_cache (hash, body, status_code, content_type, created_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(hash) DO UPDATE SET
				body = excluded.body,
				status_code = excluded.status_code,
				content_type = excluded.content_type,
				created_at = excluded.created_at,
				expires_at = excluded.expires_at
		`, key, string(entry.Body), entry.StatusCode, entry.ContentType, now.Unix(), expires.Unix())
	}
}

// Flush clears all cached entries, including persisted ones.
func (c *ExactCache) Flush() {
	c.mu.Lock()
	c.data = make(map[string]CacheEntry)
	c.mu.Unlock()
	c.hits.Store(0)
	c.misses.Store(0)
	if c.db != nil {
		_, _ = c.db.Exec(`DELETE FROM response_cache`)
	}
}

// Stats returns current cache statistics, pruning expired in-memory entries first.
func (c *ExactCache) Stats() CacheStats {
	c.pruneExpired()
	c.mu.RLock()
	size := len(c.data)
	c.mu.RUnlock()
	return CacheStats{
		Hits:   c.hits.Load(),
		Misses: c.misses.Load(),
		Size:   size,
	}
}

func (c *ExactCache) isExpired(entry CacheEntry) bool {
	ttl := c.ttlDuration()
	if ttl <= 0 {
		return false
	}
	return time.Since(entry.CreatedAt) > ttl
}

func (c *ExactCache) ensureCapacity() {
	if len(c.data) >= c.maxEntries {
		// Simple eviction: delete the first key returned by map iteration.
		// This is non-deterministic, but callers should size maxEntries
		// generously; an LRU upgrade is left for future work.
		for k := range c.data {
			delete(c.data, k)
			break
		}
	}
}

func (c *ExactCache) pruneExpired() {
	ttl := c.ttlDuration()
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, e := range c.data {
		if now.Sub(e.CreatedAt) > ttl {
			delete(c.data, k)
		}
	}
}

// loadFromDB hydrates the in-memory cache from persisted non-expired entries.
func (c *ExactCache) loadFromDB() {
	if c.db == nil {
		return
	}
	rows, err := c.db.Query(`
		SELECT hash, body, status_code, content_type, created_at
		FROM response_cache
		WHERE expires_at > ?
		ORDER BY created_at DESC
		LIMIT ?
	`, time.Now().Unix(), c.maxEntries)
	if err != nil {
		return
	}
	defer rows.Close()

	c.mu.Lock()
	defer c.mu.Unlock()
	for rows.Next() {
		var hash, body, contentType string
		var statusCode, createdAt int64
		if err := rows.Scan(&hash, &body, &statusCode, &contentType, &createdAt); err != nil {
			continue
		}
		if len(c.data) >= c.maxEntries {
			break
		}
		c.data[hash] = CacheEntry{
			Body:        []byte(body),
			StatusCode:  int(statusCode),
			ContentType: contentType,
			CreatedAt:   time.Unix(createdAt, 0),
		}
	}
}
