// Package cache provides in-memory caching for Grok CLI reasoning replay items.
package cache

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"
)

const (
	grokcliReasoningCacheTTL        = 1 * time.Hour
	grokcliReasoningCacheMaxEntries = 10240
	grokcliReasoningEvictBatchSize  = 128
)

type grokcliReasoningEntry struct {
	Items     []map[string]any
	Timestamp time.Time
}

var (
	grokcliReasoningMu      sync.RWMutex
	grokcliReasoningEntries = make(map[string]grokcliReasoningEntry)

	grokcliEvictionMu     sync.Mutex
	grokcliEvictionCancel context.CancelFunc
)

func grokcliReasoningCacheKey(modelName, sessionKey string) string {
	if sessionKey == "" {
		return ""
	}
	return "grokcli-reasoning:" + modelName + ":" + sessionKey
}

// CacheGrokCLIReasoningReplayItems stores replay items for a Grok CLI session.
func CacheGrokCLIReasoningReplayItems(ctx context.Context, modelName, sessionKey string, items []map[string]any) error {
	_ = ctx
	key := grokcliReasoningCacheKey(modelName, sessionKey)
	if key == "" || len(items) == 0 {
		return nil
	}
	grokcliReasoningMu.Lock()
	defer grokcliReasoningMu.Unlock()
	grokcliReasoningEntries[key] = grokcliReasoningEntry{Items: items, Timestamp: time.Now()}
	if len(grokcliReasoningEntries) > grokcliReasoningCacheMaxEntries {
		evictOldestGrokCLIReasoningEntries(grokcliReasoningEvictBatchSize)
	}
	return nil
}

// GetGrokCLIReasoningReplayItems retrieves replay items for a Grok CLI session.
func GetGrokCLIReasoningReplayItems(ctx context.Context, modelName, sessionKey string) ([]map[string]any, error) {
	_ = ctx
	key := grokcliReasoningCacheKey(modelName, sessionKey)
	if key == "" {
		return nil, nil
	}
	grokcliReasoningMu.RLock()
	defer grokcliReasoningMu.RUnlock()
	entry, ok := grokcliReasoningEntries[key]
	if !ok || time.Since(entry.Timestamp) > grokcliReasoningCacheTTL {
		return nil, nil
	}
	out := make([]map[string]any, len(entry.Items))
	copy(out, entry.Items)
	return out, nil
}

// PurgeGrokCLIReasoningForSession deletes the cached replay items for a single session.
func PurgeGrokCLIReasoningForSession(ctx context.Context, modelName, sessionKey string) error {
	_ = ctx
	key := grokcliReasoningCacheKey(modelName, sessionKey)
	if key == "" {
		return nil
	}
	grokcliReasoningMu.Lock()
	defer grokcliReasoningMu.Unlock()
	delete(grokcliReasoningEntries, key)
	return nil
}

// evictOldestGrokCLIReasoningEntries removes the oldest count entries in a single
// pass (O(n log n)) instead of the previous O(n*k) repeated scans.
func evictOldestGrokCLIReasoningEntries(count int) {
	if count <= 0 {
		return
	}
	if len(grokcliReasoningEntries) <= count {
		grokcliReasoningEntries = make(map[string]grokcliReasoningEntry)
		return
	}

	type item struct {
		key string
		ts  time.Time
	}
	all := make([]item, 0, len(grokcliReasoningEntries))
	for k, v := range grokcliReasoningEntries {
		all = append(all, item{key: k, ts: v.Timestamp})
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].ts.Before(all[j].ts)
	})
	if count > len(all) {
		count = len(all)
	}
	for i := 0; i < count; i++ {
		delete(grokcliReasoningEntries, all[i].key)
	}
}

// purgeExpiredGrokCLIReasoningReplayEntriesLocked removes expired entries while
// already holding the write lock. It returns the number of entries removed.
func purgeExpiredGrokCLIReasoningReplayEntriesLocked(now time.Time) int {
	removed := 0
	for k, v := range grokcliReasoningEntries {
		if now.Sub(v.Timestamp) > grokcliReasoningCacheTTL {
			delete(grokcliReasoningEntries, k)
			removed++
		}
	}
	return removed
}

// PurgeExpiredGrokCLIReasoningReplayEntries removes all expired entries.
func PurgeExpiredGrokCLIReasoningReplayEntries(ctx context.Context) error {
	_ = ctx
	grokcliReasoningMu.Lock()
	defer grokcliReasoningMu.Unlock()
	purgeExpiredGrokCLIReasoningReplayEntriesLocked(time.Now())
	return nil
}

// StartGrokCLIReasoningEviction starts a background goroutine that periodically
// purges expired Grok CLI reasoning replay entries.
//
// Calling Start more than once is a no-op; use StopGrokCLIReasoningEviction to
// restart with a different interval.
func StartGrokCLIReasoningEviction(ctx context.Context, interval time.Duration) {
	grokcliEvictionMu.Lock()
	defer grokcliEvictionMu.Unlock()
	if grokcliEvictionCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	grokcliEvictionCancel = cancel

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				grokcliReasoningMu.Lock()
				removed := purgeExpiredGrokCLIReasoningReplayEntriesLocked(time.Now())
				grokcliReasoningMu.Unlock()
				if removed > 0 {
					log.Printf("grok-cli reasoning cache: evicted %d expired entries", removed)
				}
			}
		}
	}()
}

// StopGrokCLIReasoningEviction stops the background eviction goroutine.
func StopGrokCLIReasoningEviction() {
	grokcliEvictionMu.Lock()
	defer grokcliEvictionMu.Unlock()
	if grokcliEvictionCancel != nil {
		grokcliEvictionCancel()
		grokcliEvictionCancel = nil
	}
}
