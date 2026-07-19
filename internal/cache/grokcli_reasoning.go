// Package cache provides in-memory caching for Grok CLI reasoning replay items.
package cache

import (
	"context"
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

func evictOldestGrokCLIReasoningEntries(count int) {
	if len(grokcliReasoningEntries) <= count {
		grokcliReasoningEntries = make(map[string]grokcliReasoningEntry)
		return
	}
	oldest := time.Now()
	var oldestKey string
	for i := 0; i < count; i++ {
		for k, v := range grokcliReasoningEntries {
			if v.Timestamp.Before(oldest) {
				oldest = v.Timestamp
				oldestKey = k
			}
		}
		if oldestKey != "" {
			delete(grokcliReasoningEntries, oldestKey)
			oldestKey = ""
			oldest = time.Now()
		}
	}
}

// PurgeExpiredGrokCLIReasoningReplayEntries removes all expired entries.
func PurgeExpiredGrokCLIReasoningReplayEntries(ctx context.Context) error {
	_ = ctx
	now := time.Now()
	grokcliReasoningMu.Lock()
	defer grokcliReasoningMu.Unlock()
	for k, v := range grokcliReasoningEntries {
		if now.Sub(v.Timestamp) > grokcliReasoningCacheTTL {
			delete(grokcliReasoningEntries, k)
		}
	}
	return nil
}
