package middleware

import (
	"database/sql"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/sync/singleflight"
)

// authResult holds a cached API key validation outcome.
type authResult struct {
	keyID     string
	rateLimit int
	cachedAt  time.Time
}

// AuthCache caches validated API keys in memory so the hot path avoids:
// - 2 DB queries per request (COUNT(*) + SELECT all keys)
// - a bcrypt comparison per stored key (~50-100ms of CPU each)
//
// The cache has a short TTL (30s) because keys can be added/rotated via the
// admin API; staleness only means a deleted key stays valid for ≤30s, which is
// an acceptable trade-off for eliminating per-request DB + bcrypt load.
type AuthCache struct {
	mu     sync.RWMutex
	entries map[string]*authResult
	ttl    time.Duration
	group  singleflight.Group // collapses concurrent cache-miss validations for the same key
}

// NewAuthCache creates a cache with the given TTL.
func NewAuthCache(ttl time.Duration) *AuthCache {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &AuthCache{
		entries: make(map[string]*authResult),
		ttl:     ttl,
	}
}

// Get returns a cached validation result, or nil if missing/expired.
func (c *AuthCache) Get(key string) *authResult {
	c.mu.RLock()
	r, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return nil
	}
	if time.Since(r.cachedAt) > c.ttl {
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return nil
	}
	return r
}

// Put stores a validation result.
func (c *AuthCache) Put(key, keyID string, rateLimit int) {
	c.mu.Lock()
	c.entries[key] = &authResult{
		keyID:     keyID,
		rateLimit: rateLimit,
		cachedAt:  time.Now(),
	}
	c.mu.Unlock()
}

// Invalidate removes a key (used when keys change via admin API).
func (c *AuthCache) Invalidate(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// InvalidateAll clears the entire cache (e.g. on key add/delete/rotate).
func (c *AuthCache) InvalidateAll() {
	c.mu.Lock()
	c.entries = make(map[string]*authResult)
	c.mu.Unlock()
}

// validateKey loads active keys from the DB, compares the presented key with
// bcrypt, and returns the matched key's id and rate limit. On success the
// result is cached by the caller.
func validateKey(db *sql.DB, presentedKey string) (string, int, bool) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM api_keys WHERE is_active = 1`).Scan(&count); err != nil {
		return "", 0, false
	}
	if count == 0 {
		// No keys configured — open access (fail-open until user sets up auth).
		return "", 0, false
	}

	rows, err := db.Query(`SELECT id, key_hash, rate_limit_per_min FROM api_keys WHERE is_active = 1`)
	if err != nil {
		return "", 0, false
	}
	defer rows.Close()

	var keyID string
	var rateLimit int
	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash, &rateLimit); err != nil {
			continue
		}
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(presentedKey)); err == nil {
			keyID = id
			break
		}
	}
	if keyID == "" {
		return "", 0, false
	}
	return keyID, rateLimit, true
}

// Validate collapses concurrent cache-miss validations for the same presented
// key into a single DB+bcrypt call via singleflight. This prevents a
// thundering herd at the 30s TTL boundary (cold start or key rotation) when
// hundreds of concurrent requests all miss the cache simultaneously.
func (c *AuthCache) Validate(db *sql.DB, presentedKey string) (string, int, bool) {
	if c == nil {
		return validateKey(db, presentedKey)
	}
	v, err, _ := c.group.Do(presentedKey, func() (interface{}, error) {
		keyID, rateLimit, ok := validateKey(db, presentedKey)
		return []interface{}{keyID, rateLimit, ok}, nil
	})
	if err != nil {
		return "", 0, false
	}
	res := v.([]interface{})
	return res[0].(string), res[1].(int), res[2].(bool)
}
