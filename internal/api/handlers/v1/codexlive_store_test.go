package v1

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openCodexLiveTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE codex_live_sessions (
			call_id TEXT PRIMARY KEY,
			conn_id TEXT NOT NULL,
			conn_token TEXT NOT NULL,
			model TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		);
		CREATE INDEX idx_codex_live_sessions_expires ON codex_live_sessions(expires_at);
	`); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func TestCodexLiveSessionStore_MemoryOnly(t *testing.T) {
	s := newCodexLiveSessionStore()
	s.put("call-1", "conn-1", "token-1", "cx/gpt-live-1-codex")

	got, ok := s.get("call-1")
	if !ok {
		t.Fatal("expected session to exist")
	}
	if got.connID != "conn-1" || got.connToken != "token-1" || got.model != "cx/gpt-live-1-codex" {
		t.Fatalf("unexpected session: %+v", got)
	}

	s.delete("call-1")
	if _, ok := s.get("call-1"); ok {
		t.Fatal("expected session to be deleted")
	}
}

func TestCodexLiveSessionStore_InvalidCallID(t *testing.T) {
	s := newCodexLiveSessionStore()
	s.put("bad call id!", "conn-1", "token-1", "cx/model")
	if _, ok := s.get("bad call id!"); ok {
		t.Fatal("expected invalid call id to be rejected")
	}
	if _, ok := s.get(""); ok {
		t.Fatal("expected empty call id to be rejected")
	}
}

func TestCodexLiveSessionStore_TTLOnAccess(t *testing.T) {
	db := openCodexLiveTestDB(t)
	s := newCodexLiveSessionStoreWithDB(db)

	// Insert an already-expired row directly into the DB.
	old := time.Now().Add(-2 * codexLiveSessionTTL)
	_, err := db.Exec(`INSERT INTO codex_live_sessions VALUES (?, ?, ?, ?, ?, ?)`,
		"expired-call", "conn-1", "token-1", "cx/model", old.Unix(), old.Add(codexLiveSessionTTL).Unix())
	if err != nil {
		t.Fatalf("insert expired: %v", err)
	}

	if _, ok := s.get("expired-call"); ok {
		t.Fatal("expected expired session to be rejected on access")
	}
}

func TestCodexLiveSessionStore_Persistence(t *testing.T) {
	db := openCodexLiveTestDB(t)
	s1 := newCodexLiveSessionStoreWithDB(db)
	s1.put("call-2", "conn-2", "token-2", "cx/gpt-live-1-codex")

	// Simulate process restart by creating a new store from the same DB.
	s2 := newCodexLiveSessionStoreWithDB(db)
	got, ok := s2.get("call-2")
	if !ok {
		t.Fatal("expected persisted session to be loaded")
	}
	if got.connID != "conn-2" || got.connToken != "token-2" {
		t.Fatalf("unexpected persisted session: %+v", got)
	}
}

func TestCodexLiveSessionStore_PersistenceUpdate(t *testing.T) {
	db := openCodexLiveTestDB(t)
	s := newCodexLiveSessionStoreWithDB(db)
	s.put("call-3", "conn-a", "token-a", "cx/model")
	s.put("call-3", "conn-b", "token-b", "cx/model")

	var connID string
	if err := db.QueryRow(`SELECT conn_id FROM codex_live_sessions WHERE call_id = ?`, "call-3").Scan(&connID); err != nil {
		t.Fatalf("query: %v", err)
	}
	if connID != "conn-b" {
		t.Errorf("conn_id = %q, want conn-b", connID)
	}
}

func TestCodexLiveSessionStore_DeletePersisted(t *testing.T) {
	db := openCodexLiveTestDB(t)
	s := newCodexLiveSessionStoreWithDB(db)
	s.put("call-4", "conn-1", "token-1", "cx/model")
	s.delete("call-4")

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM codex_live_sessions WHERE call_id = ?`, "call-4").Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows, got %d", count)
	}
}

func TestCodexLiveSessionStore_PurgeExpired(t *testing.T) {
	db := openCodexLiveTestDB(t)
	s := newCodexLiveSessionStoreWithDB(db)

	s.put("fresh-call", "conn-1", "token-1", "cx/model")

	// Manually insert an in-memory session with a stale timestamp so we can
	// exercise purgeExpired without involving the DB clock filter.
	s.mu.Lock()
	s.sessions["stale-call"] = codexLiveSession{
		callID:    "stale-call",
		connID:    "conn-1",
		connToken: "token-1",
		model:     "cx/model",
		createdAt: time.Now().Add(-2 * codexLiveSessionTTL),
	}
	s.mu.Unlock()

	if s.count() != 2 {
		t.Errorf("count before purge = %d, want 2", s.count())
	}

	// Also persist the stale row into DB to verify DB cleanup.
	old := time.Now().Add(-2 * codexLiveSessionTTL)
	_, err := db.Exec(`INSERT INTO codex_live_sessions VALUES (?, ?, ?, ?, ?, ?)`,
		"stale-call", "conn-1", "token-1", "cx/model", old.Unix(), old.Add(codexLiveSessionTTL).Unix())
	if err != nil {
		t.Fatalf("insert stale: %v", err)
	}

	s.purgeExpired(time.Now())
	if s.count() != 1 {
		t.Errorf("count after purge = %d, want 1", s.count())
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM codex_live_sessions WHERE call_id = ?`, "stale-call").Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 0 {
		t.Errorf("expected persisted stale row removed, got %d", count)
	}
}
