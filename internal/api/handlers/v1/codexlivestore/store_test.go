package codexlivestore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/db"
	_ "modernc.org/sqlite"
)

func setupSQLite(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := InitSQLiteSchema(database); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return database
}

func TestStoreMemory_GetPutDelete(t *testing.T) {
	s := Memory(5 * time.Second)
	ctx := context.Background()
	sess := Session{CallID: "call-1", ConnID: "c1", ConnToken: "t1", Model: "cx/gpt-live-1-codex"}
	if err := s.Put(ctx, sess); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, ok, err := s.Get(ctx, "call-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok || got.CallID != "call-1" {
		t.Fatalf("expected session, got ok=%v callID=%q", ok, got.CallID)
	}
	if err := s.Delete(ctx, "call-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, ok, _ = s.Get(ctx, "call-1")
	if ok {
		t.Fatal("expected session deleted")
	}
}

func TestStoreMemory_TTLExpiration(t *testing.T) {
	s := Memory(50 * time.Millisecond)
	ctx := context.Background()
	sess := Session{CallID: "call-ttl", ConnID: "c1", ConnToken: "t1", Model: "cx/gpt-live-1-codex"}
	if err := s.Put(ctx, sess); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, ok, _ := s.Get(ctx, "call-ttl"); !ok {
		t.Fatal("expected session before expiration")
	}
	time.Sleep(100 * time.Millisecond)
	_, ok, err := s.Get(ctx, "call-ttl")
	if err != nil {
		t.Fatalf("get error: %v", err)
	}
	if ok {
		t.Fatal("expected session expired")
	}
}

func TestStoreMemory_TTLBoundsCallerExpiresAt(t *testing.T) {
	s := Memory(50 * time.Millisecond)
	ctx := context.Background()
	sess := Session{
		CallID:    "call-bounded",
		ConnID:    "c1",
		ConnToken: "t1",
		Model:     "cx/gpt-live-1-codex",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := s.Put(ctx, sess); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, ok, _ := s.Get(ctx, "call-bounded"); !ok {
		t.Fatal("expected session before configured TTL")
	}
	time.Sleep(100 * time.Millisecond)
	_, ok, err := s.Get(ctx, "call-bounded")
	if err != nil {
		t.Fatalf("get error: %v", err)
	}
	if ok {
		t.Fatal("expected session expired at configured TTL")
	}
}

func TestStoreMemory_ConfiguredTTLOverridesShortExpiresAt(t *testing.T) {
	s := Memory(time.Hour)
	ctx := context.Background()
	sess := Session{
		CallID:    "call-override",
		ConnID:    "c1",
		ConnToken: "t1",
		Model:     "cx/gpt-live-1-codex",
		ExpiresAt: time.Now().Add(50 * time.Millisecond),
	}
	if err := s.Put(ctx, sess); err != nil {
		t.Fatalf("put: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	_, ok, err := s.Get(ctx, "call-override")
	if err != nil {
		t.Fatalf("get error: %v", err)
	}
	if !ok {
		t.Fatal("expected session still alive with configured TTL")
	}
}

func TestStoreMemory_Cleanup(t *testing.T) {
	s := Memory(50 * time.Millisecond)
	ctx := context.Background()
	for _, id := range []string{"a", "b"} {
		sess := Session{CallID: id, ConnID: "c1", ConnToken: "t1", Model: "cx/gpt-live-1-codex"}
		if err := s.Put(ctx, sess); err != nil {
			t.Fatalf("put %s: %v", id, err)
		}
	}
	time.Sleep(100 * time.Millisecond)
	removed := s.(*memoryStore).Cleanup(ctx)
	if removed != 2 {
		t.Fatalf("cleanup removed = %d, want 2", removed)
	}
}

func TestStoreSQLite_GetPutDelete(t *testing.T) {
	database := setupSQLite(t)
	s := SQLite(database, time.Hour)
	ctx := context.Background()
	sess := Session{CallID: "call-sqlite", ConnID: "c1", ConnToken: "t1", Model: "cx/gpt-live-1-codex"}
	if err := s.Put(ctx, sess); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, ok, err := s.Get(ctx, "call-sqlite")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok || got.CallID != "call-sqlite" {
		t.Fatalf("expected session, got ok=%v", ok)
	}
	if err := s.Delete(ctx, "call-sqlite"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, ok, _ = s.Get(ctx, "call-sqlite")
	if ok {
		t.Fatal("expected session deleted")
	}
}

func TestStoreSQLite_TTLExpiration(t *testing.T) {
	database := setupSQLite(t)
	s := SQLite(database, 50*time.Millisecond)
	ctx := context.Background()
	sess := Session{CallID: "call-exp", ConnID: "c1", ConnToken: "t1", Model: "cx/gpt-live-1-codex"}
	if err := s.Put(ctx, sess); err != nil {
		t.Fatalf("put: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	_, ok, err := s.Get(ctx, "call-exp")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ok {
		t.Fatal("expected session expired")
	}
}

func TestStoreSQLite_CleanExpired(t *testing.T) {
	database := setupSQLite(t)
	s := SQLite(database, 50*time.Millisecond)
	ctx := context.Background()
	for _, id := range []string{"a", "b"} {
		sess := Session{CallID: id, ConnID: "c1", ConnToken: "t1", Model: "cx/gpt-live-1-codex"}
		if err := s.Put(ctx, sess); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	time.Sleep(100 * time.Millisecond)
	removed, err := s.(*sqliteStore).CleanExpired(ctx)
	if err != nil {
		t.Fatalf("clean: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
}

func TestNew(t *testing.T) {
	ctx := context.Background()

	mem, err := New(Options{Provider: ProviderMemory, TTL: time.Hour})
	if err != nil {
		t.Fatalf("new memory: %v", err)
	}
	if err := mem.Put(ctx, Session{CallID: "x"}); err != nil {
		t.Fatalf("mem put: %v", err)
	}

	database := setupSQLite(t)
	sqlite, err := New(Options{Provider: ProviderSQLite, TTL: time.Hour, DB: database})
	if err != nil {
		t.Fatalf("new sqlite: %v", err)
	}
	if err := sqlite.Put(ctx, Session{CallID: "y"}); err != nil {
		t.Fatalf("sqlite put: %v", err)
	}

	if _, err := New(Options{Provider: "unknown"}); err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if _, err := New(Options{Provider: ProviderSQLite}); err == nil {
		t.Fatal("expected error for sqlite without db")
	}
}

func TestProviderFromEnv(t *testing.T) {
	if got := ProviderFromEnv(); got != "" {
		t.Fatalf("ProviderFromEnv should be empty placeholder, got %q", got)
	}
}

func TestInitSQLiteSchema_Idempotent(t *testing.T) {
	database := setupSQLite(t)
	if err := InitSQLiteSchema(database); err != nil {
		t.Fatalf("second init failed: %v", err)
	}
	// Ensure migration helper doesn't error on existing table.
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("db migrations failed: %v", err)
	}
}
