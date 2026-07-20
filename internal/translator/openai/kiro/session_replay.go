package kiro

import (
	"encoding/json"
	"sync"
	"time"
)

const (
	kiroSessionReplayTTL           = 5 * time.Minute
	maxKiroSessionReplayEntries    = 5000
)

// replayEntry stores a frozen copy of the first user message for a session.
// Keeping msg0 stable improves upstream cache hit rates because Kiro's cache
// key is partly derived from the conversation start state.
type replayEntry struct {
	msg0      map[string]any
	expiresAt time.Time
}

var (
	replayMu    sync.RWMutex
	replayStore = make(map[string]*replayEntry)
)

// getReplayMsg0 returns a frozen first user message if one exists and has
// not expired. Expired entries are removed lazily on access.
func getReplayMsg0(key string) map[string]any {
	replayMu.RLock()
	e := replayStore[key]
	replayMu.RUnlock()
	if e == nil {
		return nil
	}
	if time.Now().After(e.expiresAt) {
		replayMu.Lock()
		delete(replayStore, key)
		replayMu.Unlock()
		return nil
	}
	return e.msg0
}

// setReplayMsg0 stores a frozen copy of the first user message.
// If the store is at capacity, one arbitrary entry is evicted.
func setReplayMsg0(key string, msg0 map[string]any) {
	replayMu.Lock()
	defer replayMu.Unlock()
	if len(replayStore) >= maxKiroSessionReplayEntries {
		for k := range replayStore {
			delete(replayStore, k)
			break
		}
	}
	replayStore[key] = &replayEntry{
		msg0:      msg0,
		expiresAt: time.Now().Add(kiroSessionReplayTTL),
	}
}

// deepCopyMessage creates a deep copy of a Kiro message map via JSON round-trip.
func deepCopyMessage(m map[string]any) map[string]any {
	b, err := json.Marshal(m)
	if err != nil {
		return m
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return m
	}
	return out
}

// messagesEqual compares two Kiro message maps by JSON serialization.
func messagesEqual(a, b map[string]any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

// firstRealUserMessage returns the first userInputMessage from history or the
// current message, preferring history so that multi-turn requests still find
// the original first user turn.
func firstRealUserMessage(history []map[string]any, current map[string]any) map[string]any {
	for _, item := range history {
		if uim, ok := item["userInputMessage"].(map[string]any); ok {
			return map[string]any{"userInputMessage": uim}
		}
	}
	if current != nil {
		if uim, ok := current["userInputMessage"].(map[string]any); ok {
			return map[string]any{"userInputMessage": uim}
		}
	}
	return nil
}

// applySessionReplay freezes the first user message for a conversation and
// ensures it is replayed as the first history entry on subsequent turns.
// Volatile context (such as current timestamps) should only be injected into
// the current turn, not the replayed message.
func applySessionReplay(conversationID string, history []map[string]any, currentMessage map[string]any) []map[string]any {
	if conversationID == "" {
		return history
	}

	stored := getReplayMsg0(conversationID)
	if stored == nil {
		// First turn: freeze the incoming first user message but do not prepend
		// it to history; the current message already represents this turn.
		first := firstRealUserMessage(history, currentMessage)
		if first != nil {
			setReplayMsg0(conversationID, deepCopyMessage(first))
		}
		return history
	}

	// If the first history entry already matches the frozen message, avoid
	// creating a duplicate.
	if len(history) > 0 && messagesEqual(history[0], stored) {
		return history
	}
	return append([]map[string]any{stored}, history...)
}
