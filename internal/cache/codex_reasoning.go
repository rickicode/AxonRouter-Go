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
)

type codexReasoningEntry struct {
	Items     [][]byte
	Timestamp time.Time
}

var (
	codexReasoningMu      sync.RWMutex
	codexReasoningEntries = make(map[string]codexReasoningEntry)
)

// codexReasoningMaxBytes returns the effective global memory cap for the Codex
// reasoning replay cache. It honors AXON_CODEX_REASONING_MAX_BYTES.
func codexReasoningMaxBytes() int64 {
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
	var n int64
	for _, e := range codexReasoningEntries {
		n += entrySize(e)
	}
	return n
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
	codexReasoningEntries[key] = codexReasoningEntry{
		Items:     copied,
		Timestamp: time.Now(),
	}
	if len(codexReasoningEntries) > codexReasoningCacheMaxEntries {
		evictOldestCodexReasoningEntries(codexReasoningEvictBatchSize)
	}
	evictCodexReasoningBySize()
	return nil
}

// GetCodexReasoningReplayItems retrieves previously cached replay items for a
// Codex session, returning a deep copy so callers can mutate them safely.
func GetCodexReasoningReplayItems(ctx context.Context, modelName, sessionKey string) ([][]byte, error) {
	_ = ctx
	key := codexReasoningCacheKey(modelName, sessionKey)
	if key == "" {
		return nil, nil
	}
	codexReasoningMu.RLock()
	defer codexReasoningMu.RUnlock()
	entry, ok := codexReasoningEntries[key]
	if !ok || time.Since(entry.Timestamp) > codexReasoningCacheTTL {
		return nil, nil
	}
	out := make([][]byte, len(entry.Items))
	for i, it := range entry.Items {
		out[i] = append([]byte(nil), it...)
	}
	return out, nil
}

// ClearCodexReasoningReplayCache removes all Codex reasoning replay entries.
// This is intended for tests.
func ClearCodexReasoningReplayCache() {
	codexReasoningMu.Lock()
	defer codexReasoningMu.Unlock()
	codexReasoningEntries = make(map[string]codexReasoningEntry)
}

func evictOldestCodexReasoningEntries(count int) {
	if count <= 0 || len(codexReasoningEntries) == 0 {
		return
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
	for i := 0; i < count; i++ {
		delete(codexReasoningEntries, all[i].key)
	}
}

// evictCodexReasoningBySize removes oldest entries until the cache fits under
// the configured global max bytes. It runs after every write.
func evictCodexReasoningBySize() {
	maxBytes := codexReasoningMaxBytes()
	for totalCodexReasoningSize() > maxBytes && len(codexReasoningEntries) > 0 {
		evictOldestCodexReasoningEntries(codexReasoningEvictBatchSize)
	}
}
