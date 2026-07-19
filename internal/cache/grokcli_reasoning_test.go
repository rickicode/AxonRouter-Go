package cache

import (
	"context"
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
