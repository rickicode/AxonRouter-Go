package cache

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestGrokCLIReasoningCache_BasicLifecycle(t *testing.T) {
	ctx := context.Background()
	model := "grok-4.5"
	key := "session-a"

	items := []map[string]any{
		{"type": "reasoning", "encrypted_content": "enc-1"},
	}
	if err := CacheGrokCLIReasoningReplayItems(ctx, model, key, items); err != nil {
		t.Fatalf("store error: %v", err)
	}
	got, err := GetGrokCLIReasoningReplayItems(ctx, model, key)
	if err != nil {
		t.Fatalf("get error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 item, got %d", len(got))
	}
	if got[0]["encrypted_content"] != "enc-1" {
		t.Errorf("unexpected content: %v", got[0])
	}

	if err := PurgeGrokCLIReasoningForSession(ctx, model, key); err != nil {
		t.Fatalf("purge error: %v", err)
	}
	got2, _ := GetGrokCLIReasoningReplayItems(ctx, model, key)
	if len(got2) != 0 {
		t.Errorf("expected empty after purge, got %d", len(got2))
	}
}

func TestGrokCLIReasoningCache_Expiry(t *testing.T) {
	ctx := context.Background()
	model := "grok-4.5"
	key := "session-b"

	// Backdate entry through direct map mutation.
	grokcliReasoningMu.Lock()
	grokcliReasoningEntries[grokcliReasoningCacheKey(model, key)] = grokcliReasoningEntry{
		Items:     []map[string]any{{"type": "reasoning"}},
		Timestamp: time.Now().Add(-2 * time.Hour),
	}
	grokcliReasoningMu.Unlock()

	if err := PurgeExpiredGrokCLIReasoningReplayEntries(ctx); err != nil {
		t.Fatalf("purge error: %v", err)
	}
	got, _ := GetGrokCLIReasoningReplayItems(ctx, model, key)
	if len(got) != 0 {
		t.Errorf("expected expired entry removed, got %d", len(got))
	}
}

func TestGrokCLIReasoningCache_EvictOldestBatch(t *testing.T) {
	ctx := context.Background()
	model := "grok-4.5"

	// Fill the cache to its configured limit so the next insertion triggers eviction.
	const n = grokcliReasoningCacheMaxEntries + 10
	// Directly populate entries with monotonically increasing timestamps.
	grokcliReasoningMu.Lock()
	grokcliReasoningEntries = make(map[string]grokcliReasoningEntry)
	start := time.Now().Add(-time.Duration(n) * time.Second)
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("session-%08d", i)
		grokcliReasoningEntries[grokcliReasoningCacheKey(model, key)] = grokcliReasoningEntry{
			Items:     []map[string]any{{"type": "reasoning", "idx": i}},
			Timestamp: start.Add(time.Duration(i) * time.Second),
		}
	}
	grokcliReasoningMu.Unlock()

	if err := CacheGrokCLIReasoningReplayItems(ctx, model, "newest", []map[string]any{{"type": "reasoning"}}); err != nil {
		t.Fatalf("store error: %v", err)
	}

	grokcliReasoningMu.Lock()
	remaining := len(grokcliReasoningEntries)
	grokcliReasoningMu.Unlock()

	// New entry should trigger eviction of the configured batch size.
	wantRemaining := n + 1 - grokcliReasoningEvictBatchSize
	if remaining != wantRemaining {
		t.Errorf("expected %d remaining, got %d", wantRemaining, remaining)
	}

	// Oldest entries should be gone.
	grokcliReasoningMu.RLock()
	_, exists := grokcliReasoningEntries[grokcliReasoningCacheKey(model, "session-00000000")]
	grokcliReasoningMu.RUnlock()
	if exists {
		t.Errorf("expected oldest entry to be evicted")
	}

	// Newest entry should still exist.
	got, _ := GetGrokCLIReasoningReplayItems(ctx, model, "newest")
	if len(got) != 1 {
		t.Errorf("expected newest entry to survive eviction, got %d", len(got))
	}

	// Clean up so later tests don't inherit thousands of stale entries.
	grokcliReasoningMu.Lock()
	grokcliReasoningEntries = make(map[string]grokcliReasoningEntry)
	grokcliReasoningMu.Unlock()
}

func TestGrokCLIReasoningCache_BackgroundEviction(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	model := "grok-4.5"
	key := "session-bg"

	// Insert an already-expired entry.
	grokcliReasoningMu.Lock()
	grokcliReasoningEntries[grokcliReasoningCacheKey(model, key)] = grokcliReasoningEntry{
		Items:     []map[string]any{{"type": "reasoning"}},
		Timestamp: time.Now().Add(-2 * time.Hour),
	}
	grokcliReasoningMu.Unlock()

	StartGrokCLIReasoningEviction(ctx, 50*time.Millisecond)
	defer StopGrokCLIReasoningEviction()

	// Wait for at least one eviction tick.
	time.Sleep(150 * time.Millisecond)

	got, _ := GetGrokCLIReasoningReplayItems(ctx, model, key)
	if len(got) != 0 {
		t.Errorf("expected background eviction to remove expired entry, got %d", len(got))
	}
}
