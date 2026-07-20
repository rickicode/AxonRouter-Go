package v1

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/combo"
	"github.com/rickicode/AxonRouter-Go/internal/compression"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
	"github.com/rickicode/AxonRouter-Go/internal/proxypool"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

// benchExecutor is a minimal executor stub used for routing benchmarks.
type benchExecutor struct{}

func (benchExecutor) Execute(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	return &executor.Response{StatusCode: 200}, nil
}

func (benchExecutor) ExecuteStream(ctx context.Context, req *executor.Request) (*executor.StreamResult, error) {
	return nil, errors.New("streaming not supported")
}

func openBenchDB(b *testing.B) *sql.DB {
	b.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		b.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	return db
}

func newBenchHandler(b *testing.B, n int) *Handler {
	b.Helper()

	logging.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))

	store := connstate.NewStore()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := connIDFor(i)
		ids = append(ids, id)
		store.SeedConnection(id, "bench", "ready", 0)
		if cs := store.Get(id); cs != nil {
			cs.SetRemainingPct(float64(100 - i%101))
		}
	}

	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()

	database := openBenchDB(b)
	authMgr := auth.NewManager()
	providerCfg := providercfg.NewManager(b.TempDir())
	if err := providerCfg.Save("bench", providercfg.ProviderSettings{RoutingMode: providercfg.RoundRobin}); err != nil {
		b.Fatalf("save routing mode: %v", err)
	}

	h := &Handler{
			db:                  database,
			store:               store,
			elig:                elig,
			authMgr:             authMgr,
			exhaustion:          quota.NewExhaustionCache(),
			providerCfg:         providerCfg,
			combo:               combo.NewHandler(database, store, elig),
			registry:            executor.GetRegistry(),
			resolver:            &proxypool.Resolver{},
			tracker:             &usage.Tracker{},
			deviceTracker:       &usage.DeviceTracker{},
			compressionStrategy: compression.Strategy{},
			failoverMaxAttempts: 5,
		}

	h.registry.Register("bench", executor.FormatOpenAI, benchExecutor{})

	now := time.Now()
	for _, id := range ids {
		h.conns.Store(id, cachedConn{
			conn: &Connection{
				ID:       id,
				Provider: "bench",
				Name:     "bench-" + id,
			},
			cachedAt: now,
		})
	}

	return h
}

func connIDFor(i int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	const lenID = 12
	id := make([]byte, lenID)
	v := i
	for j := 0; j < lenID; j++ {
		id[j] = chars[v%len(chars)]
		v /= len(chars)
		if v == 0 {
			v = i + 1
		}
	}
	return string(id)
}

func BenchmarkGetConnection(b *testing.B) {
	sizes := []int{1, 10, 100}
	modes := []struct {
		name string
		mode providercfg.RoutingMode
	}{
		{"first_eligible", providercfg.FirstEligible},
		{"round_robin", providercfg.RoundRobin},
	}

	ctx := context.Background()
	for _, mode := range modes {
		for _, n := range sizes {
			b.Run(mode.name+"/n="+itoa(n), func(b *testing.B) {
				h := newBenchHandler(b, n)
				if err := h.providerCfg.Save("bench", providercfg.ProviderSettings{RoutingMode: mode.mode}); err != nil {
					b.Fatalf("save routing mode: %v", err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_, _ = h.getConnection(ctx, "bench", "model-x")
				}
			})
		}
	}
}

func BenchmarkTryPickConnection(b *testing.B) {
	h := newBenchHandler(b, 1)
	ctx := context.Background()
	id := connIDFor(0)
	cs := h.store.Get(id)
	if cs == nil {
		b.Fatal("benchmark connection state not found")
	}
	now := time.Now()
	mode := h.providerCfg.RoutingMode("bench")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = h.tryPickConnection(ctx, cs, "bench", "model-x", now, mode)
	}
}

func BenchmarkResolveExecutor(b *testing.B) {
	h := newBenchHandler(b, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = h.resolveExecutor("bench", "model-x")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
