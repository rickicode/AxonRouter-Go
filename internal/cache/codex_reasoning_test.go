package cache

import (
	"context"
	"testing"
)

func TestCodexReasoningCache_EvictsByTotalSize(t *testing.T) {
	ClearCodexReasoningReplayCache()
	t.Setenv("AXON_CODEX_REASONING_MAX_BYTES", "500")

	item1 := []byte(`{"reasoning":"first-item-with-enough-bytes-to-exceed-cap"}`)
	item2 := []byte(`{"reasoning":"second-item-with-enough-bytes-to-exceed-cap"}`)
	item3 := []byte(`{"reasoning":"third-item-with-enough-bytes-to-exceed-cap"}`)

	_ = CacheCodexReasoningReplayItems(context.Background(), "m1", "s1", [][]byte{item1})
	_ = CacheCodexReasoningReplayItems(context.Background(), "m1", "s2", [][]byte{item2})
	_ = CacheCodexReasoningReplayItems(context.Background(), "m1", "s3", [][]byte{item3})

	size := totalCodexReasoningSize()
	if size <= 0 {
		t.Fatal("expected some cached data")
	}
	if len(codexReasoningEntries) == 0 {
		t.Fatal("expected at least one cached entry after eviction")
	}
	if size > 500 {
		t.Fatalf("expected cache size <= 500 after eviction, got %d", size)
	}
}

func TestCodexReasoningMaxBytes_EnvironmentOverride(t *testing.T) {
	t.Setenv("AXON_CODEX_REASONING_MAX_BYTES", "12345")
	if got := codexReasoningMaxBytes(); got != 12345 {
		t.Errorf("codexReasoningMaxBytes() = %d, want 12345", got)
	}
}

func TestCodexReasoningMaxBytes_Default(t *testing.T) {
	t.Setenv("AXON_CODEX_REASONING_MAX_BYTES", "")
	if got := codexReasoningMaxBytes(); got != defaultCodexReasoningMaxBytes {
		t.Errorf("codexReasoningMaxBytes() = %d, want %d", got, defaultCodexReasoningMaxBytes)
	}
}
