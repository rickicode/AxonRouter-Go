package cache

import (
	"context"
	"testing"
	"time"
)

func TestCodexReasoningCache_BasicRoundTrip(t *testing.T) {
	ClearCodexReasoningReplayCache()
	ctx := context.Background()

	items := [][]byte{[]byte("item-a"), []byte("item-b")}
	if err := CacheCodexReasoningReplayItems(ctx, "cx/gpt-live", "sess-1", items); err != nil {
		t.Fatalf("store: %v", err)
	}

	got, err := GetCodexReasoningReplayItems(ctx, "cx/gpt-live", "sess-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 2 || string(got[0]) != "item-a" || string(got[1]) != "item-b" {
		t.Fatalf("unexpected items: %v", got)
	}
}

func TestCodexReasoningCache_PerItemLimitEnforced(t *testing.T) {
	ClearCodexReasoningReplayCache()
	ctx := context.Background()

	big := make([]byte, codexReasoningMaxBytesPerItem+1)
	ok := make([]byte, 10)
	if err := CacheCodexReasoningReplayItems(ctx, "m", "sk", [][]byte{big, ok}); err != nil {
		t.Fatalf("store: %v", err)
	}

	got, _ := GetCodexReasoningReplayItems(ctx, "m", "sk")
	if len(got) != 1 || string(got[0]) != string(ok) {
		t.Fatalf("expected only small item, got %d items", len(got))
	}
}

func TestCodexReasoningCache_PerKeyLimitEnforced(t *testing.T) {
	ClearCodexReasoningReplayCache()
	ctx := context.Background()

	// Each item is within the per-item limit, but 20 items exceed the 16 MB per-key cap.
	item := make([]byte, 1<<20) // 1 MB
	items := make([][]byte, 20)
	for i := range items {
		items[i] = item
	}

	if err := CacheCodexReasoningReplayItems(ctx, "m", "sk", items); err != nil {
		t.Fatalf("store: %v", err)
	}

	got, _ := GetCodexReasoningReplayItems(ctx, "m", "sk")
	var total int
	for _, it := range got {
		total += len(it)
	}
	if total > codexReasoningMaxBytesPerKey {
		t.Fatalf("stored %d bytes, more than per-key cap %d", total, codexReasoningMaxBytesPerKey)
	}
	if total < codexReasoningMaxBytesPerKey/2 {
		t.Fatalf("stored too little (%d bytes), expected close to per-key cap", total)
	}
}

func TestCodexReasoningCache_GlobalMemoryCapEviction(t *testing.T) {
	ClearCodexReasoningReplayCache()
	ctx := context.Background()

	// Fill enough data to exceed the 128 MB global cap. Use 1 MB items, 10 per
	// session, giving ~10 MB per key.
	item := make([]byte, 1<<20)
	for i := 0; i < 20; i++ {
		items := make([][]byte, 10)
		for j := range items {
			items[j] = item
		}
		if err := CacheCodexReasoningReplayItems(ctx, "m", "sess-"+string(rune('a'+i)), items); err != nil {
			t.Fatalf("store session %d: %v", i, err)
		}
	}

	var total int
	for _, entry := range codexReasoningEntries {
		for _, it := range entry.Items {
			total += len(it)
		}
	}
	if total > codexReasoningCacheMaxBytesTotal {
		t.Fatalf("total bytes %d exceeds cap %d", total, codexReasoningCacheMaxBytesTotal)
	}
	if total < codexReasoningCacheMaxBytesTotal/2 {
		t.Fatalf("expected significant retained bytes, got only %d", total)
	}
}

func TestCodexReasoningCache_TTLEvicts(t *testing.T) {
	ClearCodexReasoningReplayCache()
	ctx := context.Background()

	// Inject a stale entry directly to avoid sleeps.
	codexReasoningMu.Lock()
	codexReasoningEntries["codex-reasoning-replay:m:sk"] = codexReasoningEntry{
		Items:     [][]byte{[]byte("old")},
		Timestamp: time.Now().Add(-2 * codexReasoningCacheTTL),
	}
	codexReasoningMu.Unlock()

	got, _ := GetCodexReasoningReplayItems(ctx, "m", "sk")
	if got != nil {
		t.Fatalf("expected expired entry to be absent, got %v", got)
	}
}

func TestCodexReasoningCache_GetReturnsDeepCopy(t *testing.T) {
	ClearCodexReasoningReplayCache()
	ctx := context.Background()

	if err := CacheCodexReasoningReplayItems(ctx, "m", "sk", [][]byte{[]byte("x")}); err != nil {
		t.Fatalf("store: %v", err)
	}
	got, _ := GetCodexReasoningReplayItems(ctx, "m", "sk")
	if len(got) != 1 {
		t.Fatalf("expected one item")
	}
	got[0][0] = 'y'

	got2, _ := GetCodexReasoningReplayItems(ctx, "m", "sk")
	if string(got2[0]) != "x" {
		t.Fatalf("mutating returned slice affected cache: %s", string(got2[0]))
	}
}
