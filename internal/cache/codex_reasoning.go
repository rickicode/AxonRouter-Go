package cache

import (
	"container/list"
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	codexReasoningCacheTTL           = 1 * time.Hour
	codexReasoningCacheMaxEntries    = 10240
	codexReasoningEvictBatchSize     = 128
	codexReasoningMaxBytesPerItem    = 1 << 20
	codexReasoningMaxBytesPerKey     = 1 << 24 // 16 MB
	codexReasoningCacheMaxMemoryOpts = 128 << 20
)

var (
	errCodexReasoningKeyTooLarge = errors.New("codex reasoning replay key exceeds per-key size limit")
	errCodexReasoningEmptyItems  = errors.New("codex reasoning replay items empty after validation")
)

// codexReasoningClock is replaced in tests to control time.
var codexReasoningClock = time.Now

type codexReasoningEntry struct {
	Items     [][]byte
	Timestamp time.Time
	mu        sync.RWMutex // guards mutation when returning copies
}

type codexReasoningCache struct {
	mu         sync.Mutex
	entries    map[string]*list.Element
	lru        *list.List
	maxEntries int
	maxMemory  int64
	memoryUsed int64
}

type codexReasoningCacheItem struct {
	key   string
	entry *codexReasoningEntry
	size  int64
}

var (
	codexReasoningMu       sync.Mutex
	codexReasoningCacheRef *codexReasoningCache
)

func codexReasoningCacheKey(modelName, sessionKey string) string {
	modelName = strings.TrimSpace(modelName)
	sessionKey = strings.TrimSpace(sessionKey)
	if modelName == "" || sessionKey == "" {
		return ""
	}
	return "codex-reasoning-replay:" + modelName + ":" + sessionKey
}

func getCodexReasoningCache() *codexReasoningCache {
	codexReasoningMu.Lock()
	defer codexReasoningMu.Unlock()
	if codexReasoningCacheRef == nil {
		codexReasoningCacheRef = &codexReasoningCache{
			entries:    make(map[string]*list.Element),
			lru:        list.New(),
			maxEntries: codexReasoningCacheMaxEntries,
			maxMemory:  int64(codexReasoningCacheMaxMemoryOpts),
		}
	}
	return codexReasoningCacheRef
}

// SetCodexReasoningCacheGlobalMemoryCap configures the global memory cap. It
// affects existing and future entries. A non-positive value disables the cap
// (only per-item and per-key limits apply).
func SetCodexReasoningCacheGlobalMemoryCap(maxBytes int64) {
	c := getCodexReasoningCache()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxMemory = maxBytes
	c.enforceLimitsLocked()
}

// ResetCodexReasoningCache reinitializes the cache and is intended for tests.
func ResetCodexReasoningCache() {
	codexReasoningMu.Lock()
	defer codexReasoningMu.Unlock()
	codexReasoningCacheRef = nil
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
		if total > codexReasoningMaxBytesPerKey {
			return errCodexReasoningKeyTooLarge
		}
	}
	if len(copied) == 0 {
		return errCodexReasoningEmptyItems
	}

	return getCodexReasoningCache().set(key, copied)
}

func (c *codexReasoningCache) set(key string, items [][]byte) error {
	var size int64
	copyAll := make([][]byte, len(items))
	for i, it := range items {
		copyAll[i] = append([]byte(nil), it...)
		size += int64(len(it))
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict expired entries before adding new ones.
	c.purgeExpiredLocked()

	// If a single item is already larger than the global cap, we still keep it
	// unless the user has reduced the cap below the per-item limit; in that
	// case enforce the new cap. After size compaction, drop the whole entry to
	// avoid inconsistent state.
	if c.maxMemory > 0 && size > c.maxMemory {
		return errors.New("codex reasoning replay entry exceeds global memory cap")
	}

	if elem, ok := c.entries[key]; ok {
		c.updateInPlaceLocked(elem, copyAll, size)
	} else {
		c.insertLocked(key, copyAll, size)
	}

	// Evict until we are under the global memory cap and entry cap.
	c.enforceLimitsLocked()
	return nil
}

func (c *codexReasoningCache) updateInPlaceLocked(elem *list.Element, items [][]byte, newSize int64) {
	it := elem.Value.(*codexReasoningCacheItem)
	c.memoryUsed -= it.size
	it.entry.mu.Lock()
	it.entry.Items = items
	it.entry.Timestamp = codexReasoningClock()
	it.entry.mu.Unlock()
	it.size = newSize
	c.memoryUsed += newSize
	c.lru.MoveToFront(elem)
}

func (c *codexReasoningCache) insertLocked(key string, items [][]byte, size int64) {
	entry := &codexReasoningEntry{Items: items, Timestamp: codexReasoningClock()}
	item := &codexReasoningCacheItem{key: key, entry: entry, size: size}
	elem := c.lru.PushFront(item)
	c.entries[key] = elem
	c.memoryUsed += size
}

func (c *codexReasoningCache) enforceLimitsLocked() {
	for (c.maxMemory > 0 && c.memoryUsed > c.maxMemory) || len(c.entries) > c.maxEntries {
		if c.lru.Len() == 0 {
			break
		}
		elem := c.lru.Back()
		c.removeElementLocked(elem)
	}
}

func (c *codexReasoningCache) removeElementLocked(elem *list.Element) {
	it := elem.Value.(*codexReasoningCacheItem)
	delete(c.entries, it.key)
	c.lru.Remove(elem)
	c.memoryUsed -= it.size
}

// GetCodexReasoningReplayItems retrieves previously cached replay items for a
// Codex session, returning a deep copy so callers can mutate them safely.
func GetCodexReasoningReplayItems(ctx context.Context, modelName, sessionKey string) ([][]byte, error) {
	_ = ctx
	key := codexReasoningCacheKey(modelName, sessionKey)
	if key == "" {
		return nil, nil
	}
	c := getCodexReasoningCache()
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.entries[key]
	if !ok {
		return nil, nil
	}
	it := elem.Value.(*codexReasoningCacheItem)
	it.entry.mu.RLock()
	entry := it.entry
	if codexReasoningClock().Sub(entry.Timestamp) > codexReasoningCacheTTL {
		it.entry.mu.RUnlock()
		c.removeElementLocked(elem)
		return nil, nil
	}
	out := make([][]byte, len(entry.Items))
	for i, b := range entry.Items {
		out[i] = append([]byte(nil), b...)
	}
	it.entry.mu.RUnlock()

	entry.mu.Lock()
	entry.Timestamp = codexReasoningClock()
	entry.mu.Unlock()
	c.lru.MoveToFront(elem)

	return out, nil
}

// ClearCodexReasoningReplayCache removes all Codex reasoning replay entries.
// This is intended for tests.
func ClearCodexReasoningReplayCache() {
	c := getCodexReasoningCache()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*list.Element)
	c.lru.Init()
	c.memoryUsed = 0
}

func evictOldestCodexReasoningEntries(count int) {
	c := getCodexReasoningCache()
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := 0; i < count && c.lru.Len() > 0; i++ {
		c.removeElementLocked(c.lru.Back())
	}
}

// PurgeExpiredCodexReasoningReplayCache removes all expired entries.
func PurgeExpiredCodexReasoningReplayCache() {
	c := getCodexReasoningCache()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.purgeExpiredLocked()
}

func (c *codexReasoningCache) purgeExpiredLocked() {
	now := codexReasoningClock()
	var expired []string
	for key, elem := range c.entries {
		it := elem.Value.(*codexReasoningCacheItem)
		it.entry.mu.RLock()
		stale := now.Sub(it.entry.Timestamp) > codexReasoningCacheTTL
		it.entry.mu.RUnlock()
		if stale {
			expired = append(expired, key)
		}
	}
	for _, key := range expired {
		if elem, ok := c.entries[key]; ok {
			c.removeElementLocked(elem)
		}
	}
}

// CodexReasoningCacheStats returns current cache statistics.
func CodexReasoningCacheStats() (entries int, memoryBytes int64) {
	c := getCodexReasoningCache()
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries), c.memoryUsed
}

// retain for backwards compatibility with existing callers.

func legacyEvictOldestCodexReasoningEntries(count int, entries map[string]codexReasoningEntry) {
	if count <= 0 || len(entries) == 0 {
		return
	}
	type candidate struct {
		key       string
		timestamp time.Time
	}
	all := make([]candidate, 0, len(entries))
	for k, v := range entries {
		all = append(all, candidate{key: k, timestamp: v.Timestamp})
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].timestamp.Before(all[j].timestamp)
	})
	if count > len(all) {
		count = len(all)
	}
	for i := 0; i < count; i++ {
		delete(entries, all[i].key)
	}
}
