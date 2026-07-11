package cache

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "cache-test.db")
	db, err := sql.Open("sqlite", tmp)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS response_cache (
			hash TEXT PRIMARY KEY,
			body TEXT NOT NULL,
			status_code INTEGER NOT NULL,
			content_type TEXT NOT NULL DEFAULT 'application/json',
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func TestExactCacheHit(t *testing.T) {
	c := NewExactCache(10)
	key := ComputeKey([]byte(`{"model":"gpt-4"}`), "openai/gpt-4")
	if _, ok := c.Get(key); ok {
		t.Error("expected cache miss on empty cache")
	}
	c.Set(key, CacheEntry{Body: []byte(`{"ok":true}`), StatusCode: 200, ContentType: "application/json"})
	entry, ok := c.Get(key)
	if !ok {
		t.Fatal("expected cache hit after set")
	}
	if string(entry.Body) != `{"ok":true}` {
		t.Errorf("unexpected body: %s", string(entry.Body))
	}
}

func TestExactCacheTTL(t *testing.T) {
	c := NewExactCacheWithTTL(10, 50*time.Millisecond)
	key := ComputeKey([]byte(`{"messages":[{"role":"user","content":"hi"}]}`), "openai/gpt-4")
	c.Set(key, CacheEntry{Body: []byte("ok"), StatusCode: 200, ContentType: "application/json"})

	if _, ok := c.Get(key); !ok {
		t.Fatal("expected cache hit before TTL expires")
	}

	time.Sleep(100 * time.Millisecond)
	if _, ok := c.Get(key); ok {
		t.Fatal("expected cache miss after TTL expires")
	}
	if got := c.Stats().Size; got != 0 {
		t.Fatalf("expected expired entry to be removed, got size %d", got)
	}
}

func TestExactCacheCanonicalKey(t *testing.T) {
	b1 := []byte(`{"model":"openai/gpt-4","messages":[{"role":"user","content":"hi"}]}`)
	b2 := []byte(`{"messages":[{"role":"user","content":"hi"}],"model":"openai/gpt-4"}`)
	if ComputeKey(b1, "x") != ComputeKey(b2, "x") {
		t.Fatal("canonical JSON key mismatch for semantically identical requests")
	}
}

func TestPersistentCacheRoundTrip(t *testing.T) {
	db := newTestDB(t)
	c := NewPersistentCache(db, 10, time.Hour)
	key := ComputeKey([]byte(`{"messages":[]}`), "openai/gpt-4")
	c.Set(key, CacheEntry{Body: []byte("persisted"), StatusCode: 200, ContentType: "application/json"})

	// Recreate the cache to exercise loadFromDB.
	c2 := NewPersistentCache(db, 10, time.Hour)
	entry, ok := c2.Get(key)
	if !ok {
		t.Fatal("expected persisted cache hit")
	}
	if string(entry.Body) != "persisted" {
		t.Errorf("unexpected body from persistence: %s", string(entry.Body))
	}
}

func TestPersistentCacheFlush(t *testing.T) {
	db := newTestDB(t)
	c := NewPersistentCache(db, 10, time.Hour)
	key := ComputeKey([]byte(`{}`), "x")
	c.Set(key, CacheEntry{Body: []byte("x"), StatusCode: 200})
	c.Flush()

	c2 := NewPersistentCache(db, 10, time.Hour)
	if _, ok := c2.Get(key); ok {
		t.Fatal("expected cache miss after flush")
	}
}

func TestPersistentCacheDoesNotLoadExpired(t *testing.T) {
	db := newTestDB(t)
	c := NewPersistentCache(db, 10, time.Hour)
	key := ComputeKey([]byte(`{}`), "x")
	c.Set(key, CacheEntry{Body: []byte("old"), StatusCode: 200})

	// Force the persisted row to be expired.
	_, err := db.Exec(`
		INSERT INTO response_cache (hash, body, status_code, content_type, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(hash) DO UPDATE SET expires_at = excluded.expires_at
	`, key, "old", 200, "application/json", time.Now().Add(-2*time.Hour).Unix(), time.Now().Add(-time.Hour).Unix())
	if err != nil {
		t.Fatalf("update expired row: %v", err)
	}

	c2 := NewPersistentCache(db, 10, time.Hour)
	if _, ok := c2.Get(key); ok {
		t.Fatal("expected expired persisted entry to be ignored")
	}
}
