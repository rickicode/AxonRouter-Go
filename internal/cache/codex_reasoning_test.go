package cache

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestCodexReasoningCache_BasicRoundTrip(t *testing.T) {
	ResetCodexReasoningCache()
	t.Cleanup(ResetCodexReasoningCache)
	ctx := context.Background()

	items := [][]byte{[]byte("reason1"), []byte("call1")}
	if err := CacheCodexReasoningReplayItems(ctx, "cx/model", "sess-1", items); err != nil {
		t.Fatalf("cache failed: %v", err)
	}
	got, err := GetCodexReasoningReplayItems(ctx, "cx/model", "sess-1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(got) != len(items) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(items))
	}
	for i := range items {
		if string(got[i]) != string(items[i]) {
			t.Errorf("item %d = %q, want %q", i, got[i], items[i])
		}
	}
}

func TestCodexReasoningCache_MissingKey(t *testing.T) {
	ResetCodexReasoningCache()
	t.Cleanup(ResetCodexReasoningCache)
	ctx := context.Background()

	got, err := GetCodexReasoningReplayItems(ctx, "cx/model", "missing")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %d items", len(got))
	}
}

func TestCodexReasoningCache_TTLExpiration(t *testing.T) {
	ResetCodexReasoningCache()
	t.Cleanup(ResetCodexReasoningCache)
	ctx := context.Background()

	start := time.Now()
	codexReasoningClock = func() time.Time { return start }
	defer func() { codexReasoningClock = time.Now }()

	if err := CacheCodexReasoningReplayItems(ctx, "cx/model", "sess-ttl", [][]byte{[]byte("x")}); err != nil {
		t.Fatalf("cache failed: %v", err)
	}
	if _, err := GetCodexReasoningReplayItems(ctx, "cx/model", "sess-ttl"); err != nil {
		t.Fatalf("get before expiry: %v", err)
	}

	codexReasoningClock = func() time.Time { return start.Add(codexReasoningCacheTTL + time.Second) }
	got, err := GetCodexReasoningReplayItems(ctx, "cx/model", "sess-ttl")
	if err != nil {
		t.Fatalf("get after expiry: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected expired entry to be removed, got %d items", len(got))
	}
}

func TestCodexReasoningCache_PerItemLimit(t *testing.T) {
	ResetCodexReasoningCache()
	t.Cleanup(ResetCodexReasoningCache)
	ctx := context.Background()

	big := make([]byte, codexReasoningMaxBytesPerItem+1)
	err := CacheCodexReasoningReplayItems(ctx, "cx/model", "sess-big", [][]byte{big})
	if !errors.Is(err, errCodexReasoningEmptyItems) {
		t.Fatalf("expected empty items error, got %v", err)
	}
}

func TestCodexReasoningCache_PerKeyLimit(t *testing.T) {
	ResetCodexReasoningCache()
	t.Cleanup(ResetCodexReasoningCache)
	ctx := context.Background()

	item := make([]byte, codexReasoningMaxBytesPerItem)
	var items [][]byte
	for i := 0; i < 20; i++ {
		items = append(items, item)
	}
	if err := CacheCodexReasoningReplayItems(ctx, "cx/model", "sess-key", items); !errors.Is(err, errCodexReasoningKeyTooLarge) {
		t.Fatalf("expected key-too-large error, got %v", err)
	}
}

func TestCodexReasoningCache_GlobalMemoryCapEviction(t *testing.T) {
	ResetCodexReasoningCache()
	t.Cleanup(ResetCodexReasoningCache)
	ctx := context.Background()

	SetCodexReasoningCacheGlobalMemoryCap(1 << 20)

	item := make([]byte, 256<<10)
	for i := 0; i < 5; i++ {
		if err := CacheCodexReasoningReplayItems(ctx, "cx/model", fmt.Sprintf("sess-%d", i), [][]byte{item}); err != nil {
			t.Fatalf("cache %d failed: %v", i, err)
		}
	}

	_, mem := CodexReasoningCacheStats()
	if mem > 1<<20 {
		t.Fatalf("memory used %d exceeds cap %d", mem, 1<<20)
	}
	if mem <= 0 {
		t.Fatal("memory used should be positive after inserts")
	}
	got, _ := GetCodexReasoningReplayItems(ctx, "cx/model", "sess-0")
	if len(got) != 0 {
		t.Errorf("expected oldest entry evicted, found %d items", len(got))
	}
}

func TestCodexReasoningCache_GlobalMemoryCapUpdateEvicts(t *testing.T) {
	ResetCodexReasoningCache()
	t.Cleanup(ResetCodexReasoningCache)
	ctx := context.Background()

	SetCodexReasoningCacheGlobalMemoryCap(0)
	item := make([]byte, 200<<10)
	for i := 0; i < 10; i++ {
		if err := CacheCodexReasoningReplayItems(ctx, "cx/model", fmt.Sprintf("sess-%d", i), [][]byte{item}); err != nil {
			t.Fatalf("cache %d failed: %v", i, err)
		}
	}

	SetCodexReasoningCacheGlobalMemoryCap(300 << 10)

	_, mem := CodexReasoningCacheStats()
	if mem > 300<<10 {
		t.Fatalf("memory used %d exceeds lowered cap %d", mem, 300<<10)
	}
}

func TestCodexReasoningCache_MaxEntriesEviction(t *testing.T) {
	ResetCodexReasoningCache()
	t.Cleanup(ResetCodexReasoningCache)
	ctx := context.Background()

	SetCodexReasoningCacheGlobalMemoryCap(0)
	for i := 0; i < codexReasoningCacheMaxEntries+10; i++ {
		if err := CacheCodexReasoningReplayItems(ctx, "cx/model", fmt.Sprintf("sess-%d", i), [][]byte{[]byte("x")}); err != nil {
			t.Fatalf("cache %d failed: %v", i, err)
		}
	}
	n, _ := CodexReasoningCacheStats()
	if n > codexReasoningCacheMaxEntries {
		t.Fatalf("entries %d exceed max %d", n, codexReasoningCacheMaxEntries)
	}
}

func TestCodexReasoningCache_Stats(t *testing.T) {
	ResetCodexReasoningCache()
	t.Cleanup(ResetCodexReasoningCache)
	ctx := context.Background()

	if err := CacheCodexReasoningReplayItems(ctx, "cx/model", "s", [][]byte{[]byte("abc")}); err != nil {
		t.Fatalf("cache failed: %v", err)
	}
	n, mem := CodexReasoningCacheStats()
	if n != 1 {
		t.Errorf("entries = %d, want 1", n)
	}
	if mem != 3 {
		t.Errorf("memory = %d, want 3", mem)
	}
}

func TestCodexReasoningCache_UpdateRefreshesLRU(t *testing.T) {
	ResetCodexReasoningCache()
	t.Cleanup(ResetCodexReasoningCache)
	ctx := context.Background()

	SetCodexReasoningCacheGlobalMemoryCap(6)
	for i := 0; i < 3; i++ {
		if err := CacheCodexReasoningReplayItems(ctx, "cx/model", fmt.Sprintf("sess-%d", i), [][]byte{[]byte("xx")}); err != nil {
			t.Fatalf("cache %d failed: %v", i, err)
		}
	}
	// Refresh sess-0 so it survives eviction. Each entry payload is 2 bytes,
	// so total used memory is 6. Adding two more entries needs 10 bytes total,
	// which means we must evict 2 entries. The refreshed sess-0 should remain.
	if _, err := GetCodexReasoningReplayItems(ctx, "cx/model", "sess-0"); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if err := CacheCodexReasoningReplayItems(ctx, "cx/model", "sess-new-1", [][]byte{[]byte("yy")}); err != nil {
		t.Fatalf("cache new-1 failed: %v", err)
	}
	if err := CacheCodexReasoningReplayItems(ctx, "cx/model", "sess-new-2", [][]byte{[]byte("zz")}); err != nil {
		t.Fatalf("cache new-2 failed: %v", err)
	}

	got, _ := GetCodexReasoningReplayItems(ctx, "cx/model", "sess-0")
	if len(got) == 0 {
		t.Error("expected refreshed entry to survive eviction")
	}
}

func TestCodexReasoningCache_UpdateDoesNotDoubleCountMemory(t *testing.T) {
	ResetCodexReasoningCache()
	t.Cleanup(ResetCodexReasoningCache)
	ctx := context.Background()

	SetCodexReasoningCacheGlobalMemoryCap(100)
	item := []byte("12345")
	if err := CacheCodexReasoningReplayItems(ctx, "cx/model", "s", [][]byte{item}); err != nil {
		t.Fatalf("cache failed: %v", err)
	}
	if _, err := GetCodexReasoningReplayItems(ctx, "cx/model", "s"); err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if err := CacheCodexReasoningReplayItems(ctx, "cx/model", "s", [][]byte{item}); err != nil {
		t.Fatalf("cache update failed: %v", err)
	}
	_, mem := CodexReasoningCacheStats()
	if mem != 5 {
		t.Errorf("memory = %d, want 5 after update", mem)
	}
}
