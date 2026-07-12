// Package cache provides in-memory caching for Antigravity thinking signatures
// and reasoning replay items. Simplified from CLIProxyAPI's implementation
// (no external KV store, no home mode).
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// ── Signature Cache ──────────────────────────────────────────────────────────
// Caches thinking signatures by model group + text hash.
// Used by Antigravity translator to avoid re-computing signatures.

// SignatureEntry holds a cached thinking signature with timestamp.
type SignatureEntry struct {
	Signature string
	Timestamp time.Time
}

const (
	// SignatureCacheTTL is how long signatures are valid.
	SignatureCacheTTL = 3 * time.Hour
	// SignatureTextHashLen is the hex length of the hash key.
	SignatureTextHashLen = 16
	// MinValidSignatureLen is the minimum length for a valid signature.
	MinValidSignatureLen = 50
	// CacheCleanupInterval controls stale entry purge frequency.
	CacheCleanupInterval = 10 * time.Minute
)

var (
	signatureCache   sync.Map // groupKey -> *groupCache
	cacheCleanupOnce sync.Once
	replayMu sync.RWMutex
	replayEntries    = make(map[string]replayEntry)
)

type groupCache struct {
	mu      sync.RWMutex
	entries map[string]SignatureEntry
}

// hashText creates a stable, Unicode-safe key from text content.
func hashText(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])[:SignatureTextHashLen]
}

// GetModelGroup returns the model group key for signature caching.
func GetModelGroup(modelName string) string {
	lower := modelName
	for _, prefix := range []string{"gemini-", "claude-"} {
		if len(lower) > len(prefix) && lower[:len(prefix)] == prefix {
			return "gemini" // Gemini/Claude-on-Antigravity share group
		}
	}
	if len(lower) > 3 && lower[:3] == "gpt" {
		return "codex"
	}
	return "default"
}

func getOrCreateGroupCache(groupKey string) *groupCache {
	cacheCleanupOnce.Do(startCacheCleanup)
	if val, ok := signatureCache.Load(groupKey); ok {
		return val.(*groupCache)
	}
	sc := &groupCache{entries: make(map[string]SignatureEntry)}
	actual, _ := signatureCache.LoadOrStore(groupKey, sc)
	return actual.(*groupCache)
}

func startCacheCleanup() {
	go func() {
		ticker := time.NewTicker(CacheCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			purgeExpiredSignatures()
		}
	}()
}

func purgeExpiredSignatures() {
	now := time.Now()
	signatureCache.Range(func(key, value any) bool {
		sc := value.(*groupCache)
		sc.mu.Lock()
		for k, entry := range sc.entries {
			if now.Sub(entry.Timestamp) > SignatureCacheTTL {
				delete(sc.entries, k)
			}
		}
		empty := len(sc.entries) == 0
		sc.mu.Unlock()
		if empty {
			signatureCache.Delete(key)
		}
		return true
	})
}

// CacheSignature stores a thinking signature for a model group + text.
func CacheSignature(modelName, text, signature string) bool {
	if text == "" || signature == "" || len(signature) < MinValidSignatureLen {
		return false
	}
	groupKey := GetModelGroup(modelName)
	textHash := hashText(text)
	sc := getOrCreateGroupCache(groupKey)
	sc.mu.Lock()
	sc.entries[textHash] = SignatureEntry{Signature: signature, Timestamp: time.Now()}
	sc.mu.Unlock()
	return true
}

// GetCachedSignature retrieves a cached signature. Returns empty if not found/expired.
func GetCachedSignature(modelName, text string) string {
	if text == "" {
		return "skip_thought_signature_validator"
	}
	groupKey := GetModelGroup(modelName)
	textHash := hashText(text)
	sc := getOrCreateGroupCache(groupKey)
	sc.mu.RLock()
	entry, ok := sc.entries[textHash]
	sc.mu.RUnlock()
	if !ok {
		return ""
	}
	if time.Since(entry.Timestamp) > SignatureCacheTTL {
		return ""
	}
	return entry.Signature
}

// SetSignatureCacheEnabled is a no-op for now (always enabled).
// Kept for API compatibility with CLIProxyAPI callers.
func SetSignatureCacheEnabled(_ bool) {}

// ── Reasoning Replay Cache ──────────────────────────────────────────────────
// Caches reasoning replay items for stateless multi-turn conversations.

const (
	ReplayCacheTTL        = 1 * time.Hour
	ReplayCacheMaxEntries = 10240
	ReplayEvictBatchSize  = 128
)

type replayEntry struct {
	Items     [][]byte
	Timestamp time.Time
}

func replayCacheKey(modelName, sessionKey string) string {
	if sessionKey == "" {
		return ""
	}
	return GetModelGroup(modelName) + ":" + sessionKey
}

// CacheReplayItems stores reasoning replay items for a session.
func CacheReplayItems(modelName, sessionKey string, items [][]byte) bool {
	key := replayCacheKey(modelName, sessionKey)
	if key == "" || len(items) == 0 {
		return false
	}
	replayMu.Lock()
	defer replayMu.Unlock()
	replayEntries[key] = replayEntry{Items: items, Timestamp: time.Now()}
	if len(replayEntries) > ReplayCacheMaxEntries {
		evictOldestReplayEntries(ReplayEvictBatchSize)
	}
	return true
}

// GetReplayItems retrieves cached reasoning replay items.
func GetReplayItems(modelName, sessionKey string) ([][]byte, bool) {
	key := replayCacheKey(modelName, sessionKey)
	if key == "" {
		return nil, false
	}
	replayMu.RLock()
	defer replayMu.RUnlock()
	entry, ok := replayEntries[key]
	if !ok || time.Since(entry.Timestamp) > ReplayCacheTTL {
		return nil, false // expired entry will be purged by PurgeExpiredReplayEntries
	}
	return entry.Items, true
}

func evictOldestReplayEntries(count int) {
	if len(replayEntries) <= count {
		return
	}
	oldest := time.Now()
	var oldestKey string
	for i := 0; i < count; i++ {
		for k, v := range replayEntries {
			if v.Timestamp.Before(oldest) {
				oldest = v.Timestamp
				oldestKey = k
			}
		}
		if oldestKey != "" {
			delete(replayEntries, oldestKey)
			oldestKey = ""
			oldest = time.Now()
		}
	}
}

// PurgeExpiredReplayEntries removes expired replay entries.
func PurgeExpiredReplayEntries() {
	now := time.Now()
	replayMu.Lock()
	defer replayMu.Unlock()
	for k, v := range replayEntries {
		if now.Sub(v.Timestamp) > ReplayCacheTTL {
			delete(replayEntries, k)
		}
	}
}
