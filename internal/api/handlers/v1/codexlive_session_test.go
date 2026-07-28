package v1

import (
	"database/sql"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/db"
	_ "modernc.org/sqlite"
)

func openCodexLiveTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

func TestCodexLiveSessionStore_MemoryStoresAndRetrieves(t *testing.T) {
	s := newCodexLiveSessionStore()
	s.put("call-1", "conn-1", "token-1", "cx/gpt-live")

	sess, ok := s.get("call-1")
	if !ok {
		t.Fatal("session not found in memory")
	}
	if sess.callID != "call-1" || sess.connID != "conn-1" || sess.connToken != "token-1" || sess.model != "cx/gpt-live" {
		t.Fatalf("unexpected session: %+v", sess)
	}
}

func TestCodexLiveSessionStore_MemoryTTLExpired(t *testing.T) {
	s := newCodexLiveSessionStore()
	s.ttl = time.Second

	s.put("call-1", "conn-1", "token-1", "cx/gpt-live")
	time.Sleep(1100 * time.Millisecond)
	if _, ok := s.get("call-1"); ok {
		t.Fatal("expired session should not be found")
	}
}

func TestCodexLiveSessionStore_MemoryCleanupRemovesExpired(t *testing.T) {
	s := newCodexLiveSessionStore()
	s.ttl = time.Second

	s.put("call-live", "conn-1", "token-1", "cx/gpt-live")
	s.put("call-expired", "conn-2", "token-2", "cx/gpt-live")

	time.Sleep(1100 * time.Millisecond)
	s.put("call-new", "conn-3", "token-3", "cx/gpt-live")

	removed := s.cleanup()
	if removed < 1 {
		t.Fatalf("expected at least one expired session removed, got %d", removed)
	}

	if _, ok := s.get("call-expired"); ok {
		t.Fatal("expired session still present after cleanup")
	}
	if _, ok := s.get("call-new"); !ok {
		t.Fatal("fresh session missing after cleanup")
	}
}

func TestCodexLiveSessionStore_PersistenceSurvivesRestart(t *testing.T) {
	database := openCodexLiveTestDB(t)
	s := newCodexLiveSessionStore(database)
	if !s.isPersistent() {
		t.Fatal("expected store to report persistent")
	}

	s.put("call-persist", "conn-1", "token-1", "cx/gpt-live")

	// Simulate process restart by creating a fresh store against the same DB.
	s2 := newCodexLiveSessionStore(database)
	sess, ok := s2.get("call-persist")
	if !ok {
		t.Fatal("session missing after restart")
	}
	if sess.connID != "conn-1" || sess.connToken != "token-1" {
		t.Fatalf("unexpected persisted session: %+v", sess)
	}
}

func TestCodexLiveSessionStore_PersistenceDelete(t *testing.T) {
	database := openCodexLiveTestDB(t)
	s := newCodexLiveSessionStore(database)
	s.put("call-delete", "conn-1", "token-1", "cx/gpt-live")

	if _, ok := s.get("call-delete"); !ok {
		t.Fatal("session missing before delete")
	}
	s.delete("call-delete")

	if _, ok := s.get("call-delete"); ok {
		t.Fatal("session should be deleted")
	}

	// Fresh store sees the deletion too.
	s2 := newCodexLiveSessionStore(database)
	if _, ok := s2.get("call-delete"); ok {
		t.Fatal("session should be deleted across restarts")
	}
}

func TestCodexLiveSessionStore_PersistenceTTLEnforced(t *testing.T) {
	database := openCodexLiveTestDB(t)
	s := newCodexLiveSessionStore(database)
	s.ttl = time.Second
	s.put("call-expired", "conn-1", "token-1", "cx/gpt-live")

	time.Sleep(1100 * time.Millisecond)
	if _, ok := s.get("call-expired"); ok {
		t.Fatal("expired persisted session should not be returned")
	}

	// DB row should have been removed by read-time cleanup.
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM codex_live_sessions WHERE call_id = ?`, "call-expired").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected expired row to be deleted from db, got %d", count)
	}
}

func TestCodexLiveSessionStore_PersistenceCleanup(t *testing.T) {
	database := openCodexLiveTestDB(t)
	s := newCodexLiveSessionStore(database)
	s.ttl = time.Second

	s.put("call-old", "conn-1", "token-1", "cx/gpt-live")
	time.Sleep(1100 * time.Millisecond)
	s.put("call-new", "conn-2", "token-2", "cx/gpt-live")

	memRemoved := s.cleanup()
	if memRemoved < 1 {
		t.Fatalf("expected memory cleanup to remove expired, got %d", memRemoved)
	}

	if _, ok := s.get("call-old"); ok {
		t.Fatal("old session should be gone after cleanup")
	}
	if _, ok := s.get("call-new"); !ok {
		t.Fatal("new session should still be present")
	}
}
