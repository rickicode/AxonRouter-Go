package kiro

import (
	"testing"
	"time"
)

func TestApplySessionReplay_FirstTurnStoresAndDoesNotPrepend(t *testing.T) {
	key := "conv-first-turn"
	current := map[string]any{
		"userInputMessage": map[string]any{
			"content": "hello",
			"modelId": "claude-sonnet-4.6",
		},
	}

	history := applySessionReplay(key, nil, current)
	if len(history) != 0 {
		t.Errorf("first turn history expected empty, got %d", len(history))
	}

	stored := getReplayMsg0(key)
	if stored == nil {
		t.Fatalf("expected first user message to be stored")
	}
	uim, ok := stored["userInputMessage"].(map[string]any)
	if !ok || uim["content"] != "hello" {
		t.Errorf("stored message mismatch: %v", stored)
	}
}

func TestApplySessionReplay_SecondTurnPrependsFrozenMessage(t *testing.T) {
	key := "conv-second-turn"
	first := map[string]any{
		"userInputMessage": map[string]any{
			"content": "hello",
			"modelId": "claude-sonnet-4.6",
		},
	}
	setReplayMsg0(key, deepCopyMessage(first))

	history := []map[string]any{
		{"assistantOutputMessage": map[string]any{"content": "hi there"}},
	}
	history = applySessionReplay(key, history, nil)

	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}
	uim, ok := history[0]["userInputMessage"].(map[string]any)
	if !ok || uim["content"] != "hello" {
		t.Errorf("expected frozen first user message at history[0], got %v", history[0])
	}
}

func TestApplySessionReplay_AvoidsDuplicateFirstEntry(t *testing.T) {
	key := "conv-dedup"
	first := map[string]any{
		"userInputMessage": map[string]any{
			"content": "hello",
			"modelId": "claude-sonnet-4.6",
		},
	}
	setReplayMsg0(key, deepCopyMessage(first))

	history := []map[string]any{deepCopyMessage(first)}
	history = applySessionReplay(key, history, nil)
	if len(history) != 1 {
		t.Errorf("expected duplicate suppression, got %d entries", len(history))
	}
}

func TestGetReplayMsg0_ExpiresAndRemovesStaleEntry(t *testing.T) {
	key := "conv-expired"
	replayedMsg := map[string]any{
		"userInputMessage": map[string]any{
			"content": "only one turn",
		},
	}

	replayMu.Lock()
	replayStore[key] = &replayEntry{
		msg0:      deepCopyMessage(replayedMsg),
		expiresAt: time.Now().Add(-time.Second),
	}
	replayMu.Unlock()

	stored := getReplayMsg0(key)
	if stored != nil {
		t.Errorf("expected expired entry to be removed, got %v", stored)
	}

	replayMu.RLock()
	_, exists := replayStore[key]
	replayMu.RUnlock()
	if exists {
		t.Errorf("expected expired key to be deleted from store")
	}
}
