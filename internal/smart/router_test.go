package smart

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

func newRouterTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "smart-test.db") + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	database, err := sql.Open("sqlite", tmp)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	usage.InitPricing(database)
	return database
}

func seedSmartConfig(t *testing.T, database *sql.DB, cfg *VirtualModelConfig) {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	_, err = database.Exec(
		`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		SettingsKey, string(raw), time.Now().Unix(),
	)
	if err != nil {
		t.Fatalf("seed smart config: %v", err)
	}
}

func seedRequestLog(t *testing.T, database *sql.DB, modelID string, latencyMs, statusCode int, costUSD float64, inputTokens, outputTokens int64) {
	t.Helper()
	now := time.Now().UnixMilli()
	_, err := database.Exec(`
		INSERT INTO request_logs (id, timestamp, model_id, modality, latency_ms, status_code, cost_usd, input_tokens, output_tokens, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		modelID+"-"+time.Now().String(), now, modelID, "chat", latencyMs, statusCode, costUSD, inputTokens, outputTokens, now,
	)
	if err != nil {
		t.Fatalf("seed request log: %v", err)
	}
}

func readyConnection(id, prefix string) *connstate.ConnectionState {
	return &connstate.ConnectionState{
		ID:     id,
		Prefix: prefix,
		Status: connstate.StatusReady,
	}
}

func TestRouter_Resolve_Auto(t *testing.T) {
	database := newRouterTestDB(t)
	cfg := &VirtualModelConfig{
		Models: []VirtualModelEntry{
			{ID: "smart/auto", Enabled: true, Candidates: []string{
				"openai/gpt-4o",
				"openai/gpt-4o-mini",
			}},
		},
	}
	seedSmartConfig(t, database, cfg)
	// Make gpt-4o-mini cheaper and faster so auto should prefer it for a simple request.
	seedRequestLog(t, database, "openai/gpt-4o-mini", 200, 200, 0.0001, 1000, 500)
	seedRequestLog(t, database, "openai/gpt-4o", 500, 200, 0.005, 1000, 500)

	store := connstate.NewStore()
	store.Set("conn-openai", readyConnection("conn-openai", "openai"))
	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()

	router := NewRouter(database, store, elig, WithConfigTTL(0))
	body := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	resolved, err := router.Resolve(context.Background(), "smart/auto", body, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if resolved != "openai/gpt-4o-mini" {
		t.Errorf("resolved = %q, want openai/gpt-4o-mini", resolved)
	}
}

func TestRouter_Resolve_Fast(t *testing.T) {
	database := newRouterTestDB(t)
	cfg := &VirtualModelConfig{
		Models: []VirtualModelEntry{
			{ID: "smart/auto-fast", Enabled: true, Candidates: []string{
				"openai/gpt-4o",
				"google/gemini-2.5-flash",
			}},
		},
	}
	seedSmartConfig(t, database, cfg)
	// Flash has lower latency and lower cost.
	seedRequestLog(t, database, "google/gemini-2.5-flash", 120, 200, 0.00005, 1000, 500)
	seedRequestLog(t, database, "openai/gpt-4o", 400, 200, 0.005, 1000, 500)

	store := connstate.NewStore()
	store.Set("conn-openai", readyConnection("conn-openai", "openai"))
	store.Set("conn-google", readyConnection("conn-google", "google"))
	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()

	router := NewRouter(database, store, elig, WithConfigTTL(0))
	body := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	resolved, err := router.Resolve(context.Background(), "smart/auto-fast", body, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if resolved != "google/gemini-2.5-flash" {
		t.Errorf("resolved = %q, want google/gemini-2.5-flash", resolved)
	}
}

func TestRouter_Resolve_Quality(t *testing.T) {
	database := newRouterTestDB(t)
	cfg := &VirtualModelConfig{
		Models: []VirtualModelEntry{
			{ID: "smart/auto-quality", Enabled: true, Candidates: []string{
				"openai/gpt-4o-mini",
				"openai/gpt-4o",
			}},
		},
	}
	seedSmartConfig(t, database, cfg)
	// gpt-4o has much higher success rate so quality should pick it.
	seedRequestLog(t, database, "openai/gpt-4o-mini", 200, 500, 0.0001, 1000, 500)
	seedRequestLog(t, database, "openai/gpt-4o", 300, 200, 0.005, 1000, 500)

	store := connstate.NewStore()
	store.Set("conn-openai", readyConnection("conn-openai", "openai"))
	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()

	router := NewRouter(database, store, elig, WithConfigTTL(0))
	body := []byte(`{"messages":[{"role":"user","content":"reasoning required"}],"reasoning_effort":"high"}`)
	resolved, err := router.Resolve(context.Background(), "smart/auto-quality", body, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if resolved != "openai/gpt-4o" {
		t.Errorf("resolved = %q, want openai/gpt-4o", resolved)
	}
}

func TestRouter_Resolve_VisionFilter(t *testing.T) {
	database := newRouterTestDB(t)
	cfg := &VirtualModelConfig{
		Models: []VirtualModelEntry{
			{ID: "smart/auto", Enabled: true, Candidates: []string{
				"openai/gpt-4o",
				"openai/gpt-4o-mini",
			}},
		},
	}
	seedSmartConfig(t, database, cfg)

	store := connstate.NewStore()
	store.Set("conn-openai", readyConnection("conn-openai", "openai"))
	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()

	router := NewRouter(database, store, elig, WithConfigTTL(0))
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]}]}`)
	resolved, err := router.Resolve(context.Background(), "smart/auto", body, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	// Both support vision. Since no telemetry, the cheaper mini wins via pricing fallback.
	if resolved != "openai/gpt-4o-mini" {
		t.Errorf("resolved = %q, want openai/gpt-4o-mini", resolved)
	}
}

func TestRouter_Resolve_Disabled(t *testing.T) {
	database := newRouterTestDB(t)
	cfg := &VirtualModelConfig{
		Models: []VirtualModelEntry{
			{ID: "smart/auto", Enabled: false, Candidates: []string{"openai/gpt-4o"}},
		},
	}
	seedSmartConfig(t, database, cfg)
	router := NewRouter(database, nil, nil, WithConfigTTL(0))
	_, err := router.Resolve(context.Background(), "smart/auto", []byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected error for disabled virtual model")
	}
}

func TestRouter_Resolve_AllowedModels(t *testing.T) {
	database := newRouterTestDB(t)
	cfg := &VirtualModelConfig{
		Models: []VirtualModelEntry{
			{ID: "smart/auto", Enabled: true, Candidates: []string{
				"openai/gpt-4o",
				"google/gemini-2.5-flash",
			}},
		},
	}
	seedSmartConfig(t, database, cfg)

	store := connstate.NewStore()
	store.Set("conn-openai", readyConnection("conn-openai", "openai"))
	store.Set("conn-google", readyConnection("conn-google", "google"))
	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()

	router := NewRouter(database, store, elig, WithConfigTTL(0))
	allowed := map[string]struct{}{"google": {}}
	body := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	resolved, err := router.Resolve(context.Background(), "smart/auto", body, allowed)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if resolved != "google/gemini-2.5-flash" {
		t.Errorf("resolved = %q, want google/gemini-2.5-flash", resolved)
	}
}

func TestRouter_Resolve_FallbackToCombo(t *testing.T) {
	// This package does not fall back to combos; the caller does. Ensure the
	// router returns an error when no candidates are eligible so the caller
	// can continue with normal resolution.
	database := newRouterTestDB(t)
	cfg := &VirtualModelConfig{
		Models: []VirtualModelEntry{
			{ID: "smart/auto", Enabled: true, Candidates: []string{"openai/gpt-4o"}},
		},
	}
	seedSmartConfig(t, database, cfg)
	// No eligible connections.
	router := NewRouter(database, connstate.NewStore(), connstate.NewEligibilityManager(connstate.NewStore()), WithConfigTTL(0))
	_, err := router.Resolve(context.Background(), "smart/auto", []byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected error when no provider is available")
	}
}
