package connstate

import (
	"testing"
	"time"
)

func TestSessionCache_GetPut(t *testing.T) {
	sc := NewSessionCacheWithTTL(10 * time.Minute)
	defer sc.Stop()

	key := SessionKey("openai", "sess-1", "gpt-4o")
	if _, ok := sc.Get(key); ok {
		t.Fatal("expected cache miss on empty cache")
	}

	sc.Put(key, "conn-a")
	if connID, ok := sc.Get(key); !ok || connID != "conn-a" {
		t.Fatalf("expected conn-a, got %q, ok=%v", connID, ok)
	}
}

func TestSessionCache_TTLExpiration(t *testing.T) {
	sc := NewSessionCacheWithTTL(1 * time.Millisecond)
	defer sc.Stop()

	key := SessionKey("openai", "sess-2", "gpt-4o")
	sc.Put(key, "conn-b")
	if _, ok := sc.Get(key); !ok {
		t.Fatal("expected cache hit before TTL")
	}

	time.Sleep(5 * time.Millisecond)
	if _, ok := sc.Get(key); ok {
		t.Fatal("expected cache miss after TTL")
	}
}

func TestSessionCache_BackgroundCleanup(t *testing.T) {
	sc := NewSessionCacheWithTTL(1 * time.Millisecond)
	defer sc.Stop()

	key := SessionKey("openai", "sess-3", "gpt-4o")
	sc.Put(key, "conn-c")

	// Wait for cleanupLoop interval (TTL/2) plus processing time.
	time.Sleep(20 * time.Millisecond)

	sc.mu.RLock()
	_, exists := sc.entries[key]
	sc.mu.RUnlock()
	if exists {
		t.Fatal("expected expired entry to be evicted by background cleanup")
	}
}

func TestSessionKeyFormat(t *testing.T) {
	key := SessionKey("provider", "session", "model")
	want := "provider::session::model"
	if key != want {
		t.Fatalf("SessionKey = %q, want %q", key, want)
	}
}
