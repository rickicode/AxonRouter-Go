package v1

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
	provideralias "github.com/rickicode/AxonRouter-Go/internal/provider"
)

func benchmarkSetup(b *testing.B) (*Handler, context.Context) {
	logging.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))

	h := newTestHandler(b)
	now := time.Now().Unix()

	if err := h.providerCfg.Save("bench", providercfg.ProviderSettings{RoutingMode: providercfg.FirstEligible}); err != nil {
		b.Fatalf("save provider cfg: %v", err)
	}

	if _, err := h.db.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('bench','Bench','openai','http://x',?)`, now); err != nil {
		b.Fatalf("seed provider type: %v", err)
	}

	future := time.Now().Add(time.Hour)
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("bench-conn-%03d", i)
		if _, err := h.db.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES (?, 'bench', ?, 'none', 'ready', 1, ?, ?)`, id, id, now, now); err != nil {
			b.Fatalf("seed %s: %v", id, err)
		}
		h.store.SeedConnection(id, "bench", "ready", 0)
		h.store.Get(id).SetRemainingPct(50)
		if i < 99 {
			h.store.Get(id).SetModelCooldown("gpt-4o", future)
		}
	}
	// Put the single usable connection last in quota-sorted order so the hot
	// path must inspect all 99 model-cooled candidates before succeeding.
	h.store.Get("bench-conn-099").SetRemainingPct(0.1)
	h.elig.RecomputeAll()

	ctx := context.Background()
	if _, err := h.getConnection(ctx, "bench", "gpt-4o", ""); err != nil {
		b.Fatalf("warmup failed: %v", err)
	}
	return h, ctx
}

// BenchmarkGetConnection_ReadySnapshot measures the optimized routing hot path.
func BenchmarkGetConnection_ReadySnapshot(b *testing.B) {
	h, ctx := benchmarkSetup(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = h.getConnection(ctx, "bench", "gpt-4o", "")
	}
}

// BenchmarkGetConnection_ReadySnapshotOld measures the pre-optimization hot path
// where the snapshot returned IDs and every candidate required a store.Get lookup.
func BenchmarkGetConnection_ReadySnapshotOld(b *testing.B) {
	h, ctx := benchmarkSetup(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = h.getConnectionOld(ctx, "bench", "gpt-4o")
	}
}

// getConnectionOld replicates the hot path before pre-materialization: it uses
// the string-ID snapshot and re-does a store.Get lookup for every candidate.
func (h *Handler) getConnectionOld(ctx context.Context, provider string, modelID string) (*Connection, error) {
	provider = provideralias.ResolveAlias(provider)

	connIDs := h.elig.GetByPrefix(provider)
	logging.Logger.Debug("getConnection", "provider", provider, "eligible", len(connIDs))
	if len(connIDs) > 0 {
		start := h.pickStartIndex(provider, modelID, len(connIDs), h.providerCfg.RoutingMode(provider))
		bound := pickMaxAttempts
		if bound > len(connIDs) {
			bound = len(connIDs)
		}
		for i := 0; i < bound; i++ {
			idx := (start + i) % len(connIDs)
			if conn, ok := h.tryPickConnectionOld(ctx, connIDs[idx], provider, modelID); ok {
				return conn, nil
			}
		}
		for i := bound; i < len(connIDs); i++ {
			idx := (start + i) % len(connIDs)
			if conn, ok := h.tryPickConnectionOld(ctx, connIDs[idx], provider, modelID); ok {
				return conn, nil
			}
		}
	}
	return h.getConnectionFallback(ctx, provider, modelID, time.Now(), h.providerCfg.RoutingMode(provider))
}

func (h *Handler) tryPickConnectionOld(ctx context.Context, connID, provider, modelID string) (*Connection, bool) {
	cs := h.store.Get(connID)
	if cs == nil {
		return nil, false
	}
	if cs.IsInCooldownAt(time.Now()) {
		return nil, false
	}
	if modelID != "" && cs.IsModelInCooldownAt(modelID, time.Now()) {
		return nil, false
	}
	if modelID != "" && h.exhaustion.IsExhaustedScopeAt(connID, connstate.ModelScope(provider, modelID), time.Now()) {
		return nil, false
	}
	if h.exhaustion.IsExhaustedAt(connID, time.Now()) {
		return nil, false
	}
	conn, err := h.getCachedConn(ctx, connID, time.Now())
	if err != nil {
		logging.Logger.Debug("load conn failed", "conn", connID[:8], "err", err)
		return nil, false
	}
	logging.Logger.Info("getConnection selected", "provider", provider, "conn", shortID(conn.ID, 8), "name", conn.Name, "mode", h.providerCfg.RoutingMode(provider))
	h.bindActiveConn(ctx, conn)
	return conn, true
}
