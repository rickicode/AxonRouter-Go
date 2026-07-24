// Package cache provides in-memory caching for Antigravity thinking signatures
// and reasoning replay items. Simplified from CLIProxyAPI's implementation
// (no external KV store, no home mode).
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
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

// ── Antigravity Credits Balance Cache ───────────────────────────────────────

const (
	// AntigravityCreditsBalanceTTL is how long a probed Google One AI credit
	// balance remains valid before re-probing.
	AntigravityCreditsBalanceTTL = 5 * time.Minute
)

// AntigravityCreditsBalance tracks a snapshot of the Google One AI balance.
type AntigravityCreditsBalance struct {
	RemainingCredits float64
	ProbedAt         time.Time
}

var (
	antigravityCreditsBalanceMu    sync.RWMutex
	antigravityCreditsBalanceCache map[string]AntigravityCreditsBalance // keyed by authID

	antigravityCreditsFailureMu    sync.RWMutex
	antigravityCreditsFailureCache map[string]antigravityCreditsFailure // keyed by authID
)

type antigravityCreditsFailure struct {
	PermanentlyDisabled      bool
	ExplicitBalanceExhausted bool
	RecordedAt               time.Time
}

func init() {
	antigravityCreditsBalanceCache = make(map[string]AntigravityCreditsBalance)
	antigravityCreditsFailureCache = make(map[string]antigravityCreditsFailure)
}

func antigravityAuthKey(authID string) string {
	return strings.TrimSpace(authID)
}

// SetAntigravityCreditsBalance caches a Google One AI credit balance for an auth.
func SetAntigravityCreditsBalance(authID string, balance AntigravityCreditsBalance) {
	key := antigravityAuthKey(authID)
	if key == "" {
		return
	}
	antigravityCreditsBalanceMu.Lock()
	defer antigravityCreditsBalanceMu.Unlock()
	if antigravityCreditsBalanceCache == nil {
		antigravityCreditsBalanceCache = make(map[string]AntigravityCreditsBalance)
	}
	antigravityCreditsBalanceCache[key] = balance
}

// GetAntigravityCreditsBalance returns a cached balance if it exists and is not expired.
func GetAntigravityCreditsBalance(authID string) (AntigravityCreditsBalance, bool) {
	key := antigravityAuthKey(authID)
	if key == "" {
		return AntigravityCreditsBalance{}, false
	}
	antigravityCreditsBalanceMu.RLock()
	balance, ok := antigravityCreditsBalanceCache[key]
	antigravityCreditsBalanceMu.RUnlock()
	if !ok {
		return AntigravityCreditsBalance{}, false
	}
	if time.Since(balance.ProbedAt) > AntigravityCreditsBalanceTTL {
		// Best-effort delete without retry; next write will overwrite.
		antigravityCreditsBalanceMu.Lock()
		delete(antigravityCreditsBalanceCache, key)
		antigravityCreditsBalanceMu.Unlock()
		return AntigravityCreditsBalance{}, false
	}
	return balance, true
}

// MarkAntigravityCreditsPermanentlyDisabled marks an auth as permanently ineligible
// for Google One AI credits. This is used when Antigravity returns an explicit
// INSUFFICIENT_G1_CREDITS_BALANCE reason.
func MarkAntigravityCreditsPermanentlyDisabled(authID string) {
	key := antigravityAuthKey(authID)
	if key == "" {
		return
	}
	antigravityCreditsFailureMu.Lock()
	defer antigravityCreditsFailureMu.Unlock()
	if antigravityCreditsFailureCache == nil {
		antigravityCreditsFailureCache = make(map[string]antigravityCreditsFailure)
	}
	antigravityCreditsFailureCache[key] = antigravityCreditsFailure{
		PermanentlyDisabled:      true,
		ExplicitBalanceExhausted: true,
		RecordedAt:                 time.Now(),
	}
}

// IsAntigravityCreditsPermanentlyDisabled reports whether credits are permanently
// disabled for the given auth.
func IsAntigravityCreditsPermanentlyDisabled(authID string) bool {
	key := antigravityAuthKey(authID)
	if key == "" {
		return false
	}
	antigravityCreditsFailureMu.RLock()
	defer antigravityCreditsFailureMu.RUnlock()
	state, ok := antigravityCreditsFailureCache[key]
	return ok && state.PermanentlyDisabled
}

// ResetAntigravityCreditsCacheForTest clears all credits caches. Used only in tests.
func ResetAntigravityCreditsCacheForTest() {
	antigravityCreditsBalanceMu.Lock()
	antigravityCreditsBalanceCache = make(map[string]AntigravityCreditsBalance)
	antigravityCreditsBalanceMu.Unlock()

	antigravityCreditsFailureMu.Lock()
	antigravityCreditsFailureCache = make(map[string]antigravityCreditsFailure)
	antigravityCreditsFailureMu.Unlock()
}
