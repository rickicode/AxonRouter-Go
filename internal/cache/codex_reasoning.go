package cache

import (
	"context"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	codexReasoningCacheTTL        = 1 * time.Hour
	codexReasoningCacheMaxEntries = 10240
	codexReasoningEvictBatchSize  = 128
	codexReasoningMaxBytesPerItem = 1 << 20
	// defaultCodexReasoningMaxBytes is the cap on total live cache bytes.
	// Override with AXON_CODEX_REASONING_MAX_BYTES.
	defaultCodexReasoningMaxBytes = 128 << 20
	// codexReasoningCacheMaxBytes is the default global cap on cached
	// reasoning replay data (128 MB). It can be overridden via
	// SetCodexReasoningCacheMaxBytes for tests or operator tuning.
	codexReasoningCacheMaxBytes = defaultCodexReasoningMaxBytes
)

type codexReasoningEntry struct {
	Items     [][]byte
	Timestamp time.Time
}

var (
	codexReasoningMu      sync.RWMutex
	codexReasoningEntries = make(map[string]codexReasoningEntry)
	// Approximate total byte size of all cached entries. This is a
	// rough accounting (sum of item lengths) used to trigger size-based
	// eviction and guard against OOM from many large replay sessions.
	codexReasoningTotalSize int64
	// codexReasoningMaxSizeSet is true when SetCodexReasoningCacheMaxBytes has
	// been called. When true, codexReasoningMaxSizeOverride takes precedence
	// over the environment variable and the default.
	codexReasoningMaxSizeSet bool
	// codexReasoningMaxSizeOverride holds the value passed to
	// SetCodexReasoningCacheMaxBytes when codexReasoningMaxSizeSet is true.
	codexReasoningMaxSizeOverride int64
)

// codexReasoningMaxBytes returns the effective global memory cap for the Codex
// reasoning replay cache. It honors SetCodexReasoningCacheMaxBytes, then
// AXON_CODEX_REASONING_MAX_BYTES, then the default.
func codexReasoningMaxBytes() int64 {
	if codexReasoningMaxSizeSet {
		return codexReasoningMaxSizeOverride
	}
	if v := os.Getenv("AXON_CODEX_REASONING_MAX_BYTES"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return defaultCodexReasoningMaxBytes
}

func codexReasoningCacheKey(modelName, sessionKey string) string {
	modelName = strings.TrimSpace(modelName)
	sessionKey = strings.TrimSpace(sessionKey)
	if modelName == "" || sessionKey == "" {
		return ""
	}
	return "codex-reasoning-replay:" + modelName + ":" + sessionKey
}

// entrySize returns the approximate size in bytes of a cached entry.
func entrySize(e codexReasoningEntry) int64 {
	var n int64
	for _, it := range e.Items {
		n += int64(len(it))
	}
	return n
}

// totalCodexReasoningSize returns the current total byte size of the cache.
func totalCodexReasoningSize() int64 {
	codexReasoningMu.RLock()
	defer codexReasoningMu.RUnlock()
	return codexReasoningTotalSize
}

// SetCodexReasoningCacheMaxBytes overrides the global memory cap. A value
// <= 0 disables the cap (size-based eviction will never run, but entry-count
// eviction still applies). This is intended for tests and tuning.
//
// If the new cap is lower than current usage, existing entries are evicted
// immediately so the cache stays within the cap.
func SetCodexReasoningCacheMaxBytes(maxBytes int64) {
	codexReasoningMu.Lock()
	defer codexReasoningMu.Unlock()
	codexReasoningMaxSizeSet = true
	codexReasoningMaxSizeOverride = maxBytes
	evictCodexReasoningBySizeLocked()
}

// CacheCodexReasoningReplayItems stores reasoning/function_call replay items for
// a Codex session. Items are copied before storage.
func CacheCodexReasoningReplayItems(ctx context.Context, modelName, sessionKey string, items [][]byte) error {
	_ = ctx
	key := codexReasoningCacheKey(modelName, sessionKey)
	if key == "" || len(items) == 0 {
		return nil
	}
	copied := make([][]byte, 0, len(items))
	var total int
	for _, it := range items {
		if len(it) == 0 || len(it) > codexReasoningMaxBytesPerItem {
			continue
		}
		clone := append([]byte(nil), it...)
		copied = append(copied, clone)
		total += len(it)
		if total > codexReasoningMaxBytesPerItem*16 {
			break
		}
	}
	if len(copied) == 0 {
		return nil
	}
	codexReasoningMu.Lock()
	defer codexReasoningMu.Unlock()

	// Replace an existing entry: reclaim its old size before adding the new one.
	if old, ok := codexReasoningEntries[key]; ok {
		codexReasoningTotalSize -= entrySize(old)
	}
	newEntry := codexReasoningEntry{
		Items:     copied,
		Timestamp: time.Now(),
	}
	codexReasoningEntries[key] = newEntry
	codexReasoningTotalSize += entrySize(newEntry)

	// Evict by total size first to honor the memory cap, then by count.
	evictCodexReasoningBySizeLocked()
	if len(codexReasoningEntries) > codexReasoningCacheMaxEntries {
		evictOldestCodexReasoningEntries(codexReasoningEvictBatchSize)
	}
	return nil
}

// GetCodexReasoningReplayItems retrieves previously cached replay items for a
// Codex session, returning a deep copy so callers can mutate them safely.
// Expired entries discovered during reads are removed so they do not keep
// contributing to the tracked total size.
func GetCodexReasoningReplayItems(ctx context.Context, modelName, sessionKey string) ([][]byte, error) {
	_ = ctx
	key := codexReasoningCacheKey(modelName, sessionKey)
	if key == "" {
		return nil, nil
	}
	codexReasoningMu.RLock()
	entry, ok := codexReasoningEntries[key]
	if !ok {
		codexReasoningMu.RUnlock()
		return nil, nil
	}
	if time.Since(entry.Timestamp) > codexReasoningCacheTTL {
		codexReasoningMu.RUnlock()
		codexReasoningMu.Lock()
		if current, stillOk := codexReasoningEntries[key]; stillOk && time.Since(current.Timestamp) > codexReasoningCacheTTL {
			codexReasoningTotalSize -= entrySize(current)
			delete(codexReasoningEntries, key)
		}
		codexReasoningMu.Unlock()
		return nil, nil
	}
	out := make([][]byte, len(entry.Items))
	for i, it := range entry.Items {
		out[i] = append([]byte(nil), it...)
	}
	codexReasoningMu.RUnlock()
	return out, nil
}

// ClearCodexReasoningReplayCache removes all Codex reasoning replay entries.
// This is intended for tests.
func ClearCodexReasoningReplayCache() {
	codexReasoningMu.Lock()
	defer codexReasoningMu.Unlock()
	codexReasoningEntries = make(map[string]codexReasoningEntry)
	codexReasoningTotalSize = 0
	codexReasoningMaxSizeSet = false
	codexReasoningMaxSizeOverride = 0
}

func evictOldestCodexReasoningEntries(count int) int {
	if count <= 0 || len(codexReasoningEntries) == 0 {
		return 0
	}
	type candidate struct {
		key       string
		timestamp time.Time
	}
	all := make([]candidate, 0, len(codexReasoningEntries))
	for k, v := range codexReasoningEntries {
		all = append(all, candidate{key: k, timestamp: v.Timestamp})
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].timestamp.Before(all[j].timestamp)
	})
	if count > len(all) {
		count = len(all)
	}
	removed := 0
	for i := 0; i < count; i++ {
		if entry, ok := codexReasoningEntries[all[i].key]; ok {
			codexReasoningTotalSize -= entrySize(entry)
			delete(codexReasoningEntries, all[i].key)
			removed++
		}
	}
	return removed
}

// evictCodexReasoningBySizeLocked removes oldest entries until the cache fits
// under the configured global max bytes. It runs while holding
// codexReasoningMu.
func evictCodexReasoningBySizeLocked() {
	maxBytes := codexReasoningMaxBytes()
	if maxBytes <= 0 {
		return
	}
	if codexReasoningTotalSize <= maxBytes {
		return
	}
	// Sort once and evict oldest entries until the cache is back under the
	// cap. This avoids the O(n^2) cost of repeatedly sorting the map.
	type candidate struct {
		key       string
		timestamp time.Time
	}
	all := make([]candidate, 0, len(codexReasoningEntries))
	for k, v := range codexReasoningEntries {
		all = append(all, candidate{key: k, timestamp: v.Timestamp})
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].timestamp.Before(all[j].timestamp)
	})
	for _, c := range all {
		if codexReasoningTotalSize <= maxBytes {
			break
		}
		if entry, ok := codexReasoningEntries[c.key]; ok {
			codexReasoningTotalSize -= entrySize(entry)
			delete(codexReasoningEntries, c.key)
		}
	}
}
