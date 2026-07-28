package cache

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"
)

func TestCodexReasoningCache_BasicLifecycle(t *testing.T) {
	ClearCodexReasoningReplayCache()
	defer ClearCodexReasoningReplayCache()

	ctx := context.Background()
	model := "codex-gpt-5"
	session := "session-a"
	items := [][]byte{
		[]byte("reasoning item 1"),
		[]byte("reasoning item 2"),
	}

	if err := CacheCodexReasoningReplayItems(ctx, model, session, items); err != nil {
		t.Fatalf("store error: %v", err)
	}

	got, err := GetCodexReasoningReplayItems(ctx, model, session)
	if err != nil {
		t.Fatalf("get error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 items, got %d", len(got))
	}
	for i := range got {
		if !bytes.Equal(got[i], items[i]) {
			t.Errorf("item %d mismatch: got %q, want %q", i, got[i], items[i])
		}
	}
}

func TestCodexReasoningCache_TotalSizeTracking(t *testing.T) {
	ClearCodexReasoningReplayCache()
	defer ClearCodexReasoningReplayCache()

	ctx := context.Background()
	model := "codex-gpt-5"

	wantSize := int64(0)
	for i := 0; i < 5; i++ {
		item := []byte(fmt.Sprintf("payload-%d", i))
		wantSize += int64(len(item))
		if err := CacheCodexReasoningReplayItems(ctx, model, fmt.Sprintf("session-%d", i), [][]byte{item}); err != nil {
			t.Fatalf("store error: %v", err)
		}
	}

	codexReasoningMu.RLock()
	got := codexReasoningTotalSize
	codexReasoningMu.RUnlock()
	if got != wantSize {
		t.Errorf("expected total size %d, got %d", wantSize, got)
	}
}

func TestCodexReasoningCache_EvictsByTotalSize(t *testing.T) {
	ClearCodexReasoningReplayCache()
	defer ClearCodexReasoningReplayCache()

	// Cap fits one ~80-byte entry but not two.
	t.Setenv("AXON_CODEX_REASONING_MAX_BYTES", "120")

	item1 := []byte(`{"reasoning":"first-item-with-enough-bytes-to-force-eviction-abc"}`)
	item2 := []byte(`{"reasoning":"second-item-with-enough-bytes-to-force-eviction-xyz"}`)
	item3 := []byte(`{"reasoning":"third-item-with-enough-bytes-to-force-eviction-123"}`)

	_ = CacheCodexReasoningReplayItems(context.Background(), "m1", "s1", [][]byte{item1})
	_ = CacheCodexReasoningReplayItems(context.Background(), "m1", "s2", [][]byte{item2})
	_ = CacheCodexReasoningReplayItems(context.Background(), "m1", "s3", [][]byte{item3})

	size := totalCodexReasoningSize()
	if size <= 0 {
		t.Fatal("expected some cached data")
	}
	if len(codexReasoningEntries) != 1 {
		t.Fatalf("expected 1 cached entry after eviction, got %d", len(codexReasoningEntries))
	}
	if size > 120 {
		t.Fatalf("expected cache size <= 120 after eviction, got %d", size)
	}

	// Oldest entry should be evicted; newest should survive.
	first, _ := GetCodexReasoningReplayItems(context.Background(), "m1", "s1")
	if len(first) != 0 {
		t.Errorf("expected oldest entry evicted, got %d items", len(first))
	}
	third, _ := GetCodexReasoningReplayItems(context.Background(), "m1", "s3")
	if len(third) != 1 || !bytes.Equal(third[0], item3) {
		t.Errorf("expected newest entry to survive eviction")
	}
}

func TestCodexReasoningCache_SizeBasedEviction(t *testing.T) {
	ClearCodexReasoningReplayCache()
	defer ClearCodexReasoningReplayCache()

	// Tight cap that can hold at most one 600 KB entry.
	SetCodexReasoningCacheMaxBytes(700 << 10)
	defer SetCodexReasoningCacheMaxBytes(codexReasoningCacheMaxBytes)

	ctx := context.Background()
	model := "codex-gpt-5"

	first := bytes.Repeat([]byte("a"), 600<<10)
	if err := CacheCodexReasoningReplayItems(ctx, model, "first", [][]byte{first}); err != nil {
		t.Fatalf("store error: %v", err)
	}

	// Slightly newer second entry.
	time.Sleep(10 * time.Millisecond)
	second := bytes.Repeat([]byte("b"), 600<<10)
	if err := CacheCodexReasoningReplayItems(ctx, model, "second", [][]byte{second}); err != nil {
		t.Fatalf("store error: %v", err)
	}

	codexReasoningMu.RLock()
	size := codexReasoningTotalSize
	count := len(codexReasoningEntries)
	codexReasoningMu.RUnlock()

	if count != 1 {
		t.Errorf("expected 1 entry after size-based eviction, got %d", count)
	}
	if size > (700 << 10) {
		t.Errorf("expected total size <= 700 KB, got %d", size)
	}

	firstGot, _ := GetCodexReasoningReplayItems(ctx, model, "first")
	if len(firstGot) != 0 {
		t.Errorf("expected oldest entry evicted, got %d items", len(firstGot))
	}
	secondGot, _ := GetCodexReasoningReplayItems(ctx, model, "second")
	if len(secondGot) != 1 || !bytes.Equal(secondGot[0], second) {
		t.Errorf("expected newest entry to survive eviction")
	}
}

func TestCodexReasoningCache_EntryCountEvictionStillWorks(t *testing.T) {
	ClearCodexReasoningReplayCache()
	defer ClearCodexReasoningReplayCache()

	// Disable size-based eviction so only count-based eviction runs.
	SetCodexReasoningCacheMaxBytes(-1)
	defer SetCodexReasoningCacheMaxBytes(codexReasoningCacheMaxBytes)

	ctx := context.Background()
	model := "codex-gpt-5"

	const n = codexReasoningCacheMaxEntries + 10
	for i := 0; i < n; i++ {
		if err := CacheCodexReasoningReplayItems(ctx, model, fmt.Sprintf("session-%08d", i), [][]byte{[]byte("x")}); err != nil {
			t.Fatalf("store error: %v", err)
		}
	}

	codexReasoningMu.RLock()
	count := len(codexReasoningEntries)
	codexReasoningMu.RUnlock()

	if count > codexReasoningCacheMaxEntries {
		t.Errorf("expected at most %d entries after count-based eviction, got %d", codexReasoningCacheMaxEntries, count)
	}
}

func TestCodexReasoningCache_ReplaceEntryAdjustsSize(t *testing.T) {
	ClearCodexReasoningReplayCache()
	defer ClearCodexReasoningReplayCache()

	SetCodexReasoningCacheMaxBytes(-1)
	defer SetCodexReasoningCacheMaxBytes(codexReasoningCacheMaxBytes)

	ctx := context.Background()
	model := "codex-gpt-5"
	session := "session-replace"

	if err := CacheCodexReasoningReplayItems(ctx, model, session, [][]byte{bytes.Repeat([]byte("a"), 100)}); err != nil {
		t.Fatalf("store error: %v", err)
	}
	if err := CacheCodexReasoningReplayItems(ctx, model, session, [][]byte{bytes.Repeat([]byte("b"), 200)}); err != nil {
		t.Fatalf("store error: %v", err)
	}

	codexReasoningMu.RLock()
	size := codexReasoningTotalSize
	count := len(codexReasoningEntries)
	codexReasoningMu.RUnlock()

	if count != 1 {
		t.Fatalf("expected 1 entry, got %d", count)
	}
	if size != 200 {
		t.Errorf("expected total size 200 after replace, got %d", size)
	}
}

func TestCodexReasoningCache_Expiry(t *testing.T) {
	ClearCodexReasoningReplayCache()
	defer ClearCodexReasoningReplayCache()

	ctx := context.Background()
	model := "codex-gpt-5"
	session := "session-expired"

	// Backdate entry through direct map mutation to test TTL expiry without sleeping.
	codexReasoningMu.Lock()
	codexReasoningEntries[codexReasoningCacheKey(model, session)] = codexReasoningEntry{
		Items:     [][]byte{[]byte("old")},
		Timestamp: time.Now().Add(-2 * time.Hour),
	}
	codexReasoningTotalSize = int64(len("old"))
	codexReasoningMu.Unlock()

	got, err := GetCodexReasoningReplayItems(ctx, model, session)
	if err != nil {
		t.Fatalf("get error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected expired entry removed, got %d", len(got))
	}

	codexReasoningMu.RLock()
	remainingSize := codexReasoningTotalSize
	remainingCount := len(codexReasoningEntries)
	codexReasoningMu.RUnlock()
	if remainingSize != 0 || remainingCount != 0 {
		t.Errorf("expected expired entry to reduce tracked size/count to zero, got size=%d count=%d", remainingSize, remainingCount)
	}
}

func TestCodexReasoningCache_SetMaxBytesShrinksImmediately(t *testing.T) {
	ClearCodexReasoningReplayCache()
	defer ClearCodexReasoningReplayCache()

	// Start with size eviction disabled so we can stash several entries.
	SetCodexReasoningCacheMaxBytes(-1)
	defer SetCodexReasoningCacheMaxBytes(codexReasoningCacheMaxBytes)

	ctx := context.Background()
	model := "codex-gpt-5"
	for i := 0; i < 5; i++ {
		CacheCodexReasoningReplayItems(ctx, model, fmt.Sprintf("session-%d", i), [][]byte{bytes.Repeat([]byte{byte(i)}, 100<<10)})
	}

	codexReasoningMu.RLock()
	before := codexReasoningTotalSize
	codexReasoningMu.RUnlock()
	if before == 0 {
		t.Fatal("expected positive cache usage before shrink")
	}
	if before <= 200<<10 {
		t.Fatalf("expected usage > 200 KB to make test meaningful, got %d", before)
	}

	// Shrink the cap below current usage; eviction should run immediately.
	SetCodexReasoningCacheMaxBytes(200 << 10)

	codexReasoningMu.RLock()
	after := codexReasoningTotalSize
	count := len(codexReasoningEntries)
	codexReasoningMu.RUnlock()
	if after > (200 << 10) {
		t.Errorf("expected total size <= 200 KB after shrinking cap, got %d", after)
	}
	if count == 0 {
		t.Errorf("expected at least the newest entry to remain after shrink")
	}
}

func TestCodexReasoningMaxBytes_EnvironmentOverride(t *testing.T) {
	ClearCodexReasoningReplayCache()
	defer ClearCodexReasoningReplayCache()

	t.Setenv("AXON_CODEX_REASONING_MAX_BYTES", "12345")
	if got := codexReasoningMaxBytes(); got != 12345 {
		t.Errorf("codexReasoningMaxBytes() = %d, want 12345", got)
	}
}

func TestCodexReasoningMaxBytes_Default(t *testing.T) {
	ClearCodexReasoningReplayCache()
	defer ClearCodexReasoningReplayCache()

	t.Setenv("AXON_CODEX_REASONING_MAX_BYTES", "")
	if got := codexReasoningMaxBytes(); got != defaultCodexReasoningMaxBytes {
		t.Errorf("codexReasoningMaxBytes() = %d, want %d", got, defaultCodexReasoningMaxBytes)
	}
}
