package v1

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	"database/sql"
	"log/slog"
	_ "modernc.org/sqlite"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/combo"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
)

func openBenchDB(b *testing.B) *sql.DB {
	b.Helper()
	tmp := filepath.Join(b.TempDir(), "handler-bench.db")
	database, err := sql.Open("sqlite", tmp)
	if err != nil {
		b.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(1)
	if err := db.RunMigrations(database); err != nil {
		b.Fatalf("migrate: %v", err)
	}
	return database
}

func newBenchHandler(b *testing.B, connCount int) *Handler {
	b.Helper()
	logging.SetLogger(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})))

	store := connstate.NewStore()
	database := openBenchDB(b)
	b.Cleanup(func() { database.Close() })

	if _, err := database.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('bp','BP','openai','http://x',0)`); err != nil {
		b.Fatalf("seed provider type: %v", err)
	}
	now := db.UnixNow()
	for i := 0; i < connCount; i++ {
		id := benchConnID(i)
		if _, err := database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES (?, 'bp', ?, 'none', 'ready', 1, ?, ?)`, id, id, now, now); err != nil {
			b.Fatalf("seed %s: %v", id, err)
		}
		store.SeedConnection(id, "bp", "ready", 0)
	}

	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()

	h := &Handler{
		db:                  database,
		store:               store,
		elig:                elig,
		authMgr:             auth.NewManager(),
		exhaustion:          quota.NewExhaustionCache(),
		providerCfg:         providercfg.NewManager(b.TempDir()),
		combo:               combo.NewHandler(database, store, elig),
		registry:            executor.GetRegistry(),
		failoverMaxAttempts: 5,
	}

	// Warm the connection cache so the benchmark measures routing, not DB loads.
	ctx := context.Background()
	for i := 0; i < connCount; i++ {
		if _, err := h.getCachedConn(ctx, benchConnID(i), time.Now()); err != nil {
			b.Fatalf("warm cache %s: %v", benchConnID(i), err)
		}
	}
	return h
}

func benchConnID(i int) string {
	return benchConnIDs[i%len(benchConnIDs)]
}

// Pre-generated stable IDs to avoid allocation in the hot loop.
var benchConnIDs = func() []string {
	ids := make([]string, 100)
	for i := range ids {
		ids[i] = benchConnIDRaw(i)
	}
	return ids
}()

func benchConnIDRaw(i int) string {
	// Fixed-width IDs so string allocation is not part of the measured path.
	const prefix = "conn-bench-"
	return prefix + string(rune('0'+i/10)) + string(rune('0'+i%10))
}

func BenchmarkTryPickConnection(b *testing.B) {
	h := newBenchHandler(b, 10)
	ctx := context.Background()
	now := time.Now()
	connID := benchConnID(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, ok := h.tryPickConnection(ctx, connID, "bp", "gpt-4o", now)
		if !ok {
			b.Fatal("expected connection to be picked")
		}
	}
}
