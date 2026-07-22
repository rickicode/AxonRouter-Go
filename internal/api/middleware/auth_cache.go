package middleware

import (
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/sync/singleflight"
)

// authResult holds a cached API key validation outcome.
type authResult struct {
	keyID         string
	rateLimit     int
	maxTokens     int64
	expiresAt     int64
	allowedModels map[string]struct{}
	cachedAt      time.Time
}

// AuthCache caches validated API keys in memory so the hot path avoids:
// - 2 DB queries per request (COUNT(*) + SELECT all keys)
// - a bcrypt comparison per stored key (~50-100ms of CPU each)
//
// The cache has a short TTL (30s) because keys can be added/rotated via the
// admin API; staleness only means a deleted key stays valid for ≤30s, which is
// an acceptable trade-off for eliminating per-request DB + bcrypt load.
type AuthCache struct {
	mu      sync.RWMutex
	entries map[string]*authResult
	ttl     time.Duration
	group   singleflight.Group // collapses concurrent cache-miss validations for the same key
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
	if time.Since(r.cachedAt) <= c.ttl {
		return r
	}
	// Recheck expiry under the write lock to avoid deleting an entry that was
	// refreshed between the RUnlock and Lock (TOCTOU).
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.entries[key]; ok && time.Since(r.cachedAt) <= c.ttl {
		return r
	}
	delete(c.entries, key)
	return nil
}

// Put stores a validation result.
func (c *AuthCache) Put(key, keyID string, rateLimit int, maxTokens int64, expiresAt int64, allowedModels map[string]struct{}) {
	c.mu.Lock()
	c.entries[key] = &authResult{
		keyID:         keyID,
		rateLimit:     rateLimit,
		maxTokens:     maxTokens,
		expiresAt:     expiresAt,
		allowedModels: allowedModels,
		cachedAt:      time.Now(),
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

// parseAllowedModels JSON-unmarshals the raw allowed_models value into a set.
// Empty or invalid JSON is treated as unlimited (empty set), matching the
// semantics of a null/empty allowed_models column.
func parseAllowedModels(raw string) map[string]struct{} {
	if raw == "" {
		return map[string]struct{}{}
	}
	var models []string
	if err := json.Unmarshal([]byte(raw), &models); err != nil {
		return map[string]struct{}{}
	}
	set := make(map[string]struct{}, len(models))
	for _, m := range models {
		set[m] = struct{}{}
	}
	return set
}

// validateKey loads active keys from the DB, compares the presented key with
// bcrypt, and returns the matched key's id, rate limit, and expiry. The returned
// error signals a DB failure; callers should treat it as an auth-system outage.
// expired is true when the presented key matches but is past its expires_at.
func validateKey(db *sql.DB, presentedKey string) (string, int, int64, int64, map[string]struct{}, bool, bool, error) {
	now := time.Now().Unix()
	rows, err := db.Query(`SELECT id, key_hash, rate_limit_per_min, COALESCE(max_tokens, 0), COALESCE(expires_at, 0), COALESCE(allowed_models, '') FROM api_keys WHERE is_active = 1`)
	if err != nil {
		return "", 0, 0, 0, nil, false, false, err
	}
	defer rows.Close()

	var keyID string
	var rateLimit int
	var maxTokens int64
	var expiresAt int64
	var allowedModels map[string]struct{}
	var matchedExpired bool
	for rows.Next() {
		var id, hash string
		var rowExpires int64
		var allowedModelsRaw string
		if err := rows.Scan(&id, &hash, &rateLimit, &maxTokens, &rowExpires, &allowedModelsRaw); err != nil {
			logging.Logger.Warn("auth cache scan error", "error", err)
			continue
		}
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(presentedKey)); err == nil {
			if rowExpires > 0 && now >= rowExpires {
				matchedExpired = true
				continue
			}
			keyID = id
			expiresAt = rowExpires
			allowedModels = parseAllowedModels(allowedModelsRaw)
			break
		}
	}
	if keyID != "" {
		return keyID, rateLimit, maxTokens, expiresAt, allowedModels, true, false, nil
	}
	if matchedExpired {
		return "", 0, 0, 0, nil, false, true, nil
	}
	return "", 0, 0, 0, nil, false, false, nil
}

// Validate collapses concurrent cache-miss validations for the same presented
// key into a single DB+bcrypt call via singleflight. This prevents a
// thundering herd at the 30s TTL boundary (cold start or key rotation) when
// hundreds of concurrent requests all miss the cache simultaneously.
func (c *AuthCache) Validate(db *sql.DB, presentedKey string) (string, int, int64, map[string]struct{}, bool, bool, error) {
	if c == nil {
		keyID, rateLimit, maxTokens, _, allowedModels, ok, expired, dbErr := validateKey(db, presentedKey)
		return keyID, rateLimit, maxTokens, allowedModels, ok, expired, dbErr
	}
	v, err, _ := c.group.Do(presentedKey, func() (interface{}, error) {
		keyID, rateLimit, maxTokens, expiresAt, allowedModels, ok, expired, dbErr := validateKey(db, presentedKey)
		if dbErr != nil {
			return nil, dbErr
		}
		if ok {
			c.Put(presentedKey, keyID, rateLimit, maxTokens, expiresAt, allowedModels)
		}
		return []interface{}{keyID, rateLimit, maxTokens, allowedModels, ok, expired}, nil
	})
	if err != nil {
		return "", 0, 0, nil, false, false, err
	}
	res := v.([]interface{})
	return res[0].(string), res[1].(int), res[2].(int64), res[3].(map[string]struct{}), res[4].(bool), res[5].(bool), nil
}
