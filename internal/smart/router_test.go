package smart

import (
	"database/sql"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
)

func setupTestRouter() (*Router, *connstate.Store) {
	store := connstate.NewStore()
	cs := &connstate.ConnectionState{
		ID:       "conn-openai-1",
		Prefix:   "openai",
		Status:   connstate.StatusReady,
		Priority: 1,
	}
	store.Set(cs.ID, cs)
	reg := NewRegistry(nil)
	// set explicit candidates so routing is deterministic
	_, _ = reg.Upsert(&VirtualModel{
		ID:         ModelAuto,
		Candidates: []string{"openai/gpt-4o-mini", "openai/gpt-4o"},
		Enabled:    true,
		Strategy:   StrategyBalanced,
	})
	_, _ = reg.Upsert(&VirtualModel{
		ID:         ModelAutoFast,
		Candidates: []string{"openai/gpt-4o-mini"},
		Enabled:    true,
		Strategy:   StrategyFast,
	})
	_, _ = reg.Upsert(&VirtualModel{
		ID:         ModelAutoQuality,
		Candidates: []string{"openai/gpt-4o"},
		Enabled:    true,
		Strategy:   StrategyQuality,
	})
	tel := NewTelemetryStore(nil)
	tel.SetTTL(0)
	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()
	r := NewRouter(reg, tel, store, elig)
	return r, store
}

func TestRouter_ResolveAuto(t *testing.T) {
	r, _ := setupTestRouter()
	body := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	res, err := r.Resolve("smart/auto", body)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if res.Provider != "openai" {
		t.Errorf("provider = %q, want openai", res.Provider)
	}
	if res.ConnectionID == "" {
		t.Error("expected non-empty connection id")
	}
}

func TestRouter_ResolveFast(t *testing.T) {
	r, _ := setupTestRouter()
	body := []byte(`{"messages":[{"role":"user","content":"quick"}]}`)
	res, err := r.Resolve("smart/auto-fast", body)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if res.ModelID != "gpt-4o-mini" {
		t.Errorf("model = %q, want gpt-4o-mini", res.ModelID)
	}
}

func TestRouter_ResolveQuality(t *testing.T) {
	r, _ := setupTestRouter()
	body := []byte(`{"reasoning_effort":"high","messages":[{"role":"user","content":"deep"}]}`)
	res, err := r.Resolve("smart/auto-quality", body)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if res.ModelID != "gpt-4o" {
		t.Errorf("model = %q, want gpt-4o", res.ModelID)
	}
}

func TestRouter_ResolveDisabled(t *testing.T) {
	r, _ := setupTestRouter()
	_ = r.registry.SetEnabled(ModelAuto, false)
	_, err := r.Resolve("smart/auto", []byte(`{"messages":[]}`))
	if err == nil {
		t.Fatal("expected error for disabled model")
	}
}

func TestRouter_NoConnection(t *testing.T) {
	store := connstate.NewStore()
	reg := NewRegistry(nil)
	tel := NewTelemetryStore(nil)
	tel.SetTTL(0)
	r := NewRouter(reg, tel, store, connstate.NewEligibilityManager(store))
	_, _ = reg.Upsert(&VirtualModel{
		ID:         ModelAuto,
		Candidates: []string{"unknown/model"},
		Enabled:    true,
		Strategy:   StrategyBalanced,
	})
	_, err := r.Resolve("smart/auto", []byte(`{"messages":[]}`))
	if err == nil {
		t.Fatal("expected error when no eligible connection")
	}
}

func TestRouter_UnknownModel(t *testing.T) {
	r, _ := setupTestRouter()
	_, err := r.Resolve("smart/custom", []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for unknown virtual model")
	}
}

func TestTelemetryStore_Get(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, _ = db.Exec(`CREATE TABLE request_logs (id TEXT PRIMARY KEY, timestamp INTEGER, connection_id TEXT, provider_type_id TEXT, model_id TEXT, combo_id TEXT, modality TEXT, input_tokens INTEGER, output_tokens INTEGER, reasoning_tokens INTEGER, stream INTEGER, latency_ms INTEGER, status_code INTEGER, error_message TEXT, cost_usd REAL, client_ip TEXT, user_agent TEXT, service_tier TEXT, created_at INTEGER)`)
	_, _ = db.Exec(`INSERT INTO request_logs VALUES ('1',?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, time.Now().UnixMilli(), "c1", "openai", "openai/gpt-4o", "", "chat", 1000, 500, 0, 0, 1200, 200, "", 0.05, "", "", "", time.Now().UnixMilli())

	ts := NewTelemetryStore(db)
	ts.SetTTL(0)
	ts.SetWindow(time.Hour)
	tel := ts.Get("openai/gpt-4o")
	if tel == nil {
		t.Fatal("expected telemetry")
	}
	if tel.TotalReqs != 1 {
		t.Errorf("TotalReqs = %d, want 1", tel.TotalReqs)
	}
	if tel.SuccessRate != 1.0 {
		t.Errorf("SuccessRate = %v, want 1.0", tel.SuccessRate)
	}
	if tel.AvgLatencyMs != 1200 {
		t.Errorf("AvgLatencyMs = %v, want 1200", tel.AvgLatencyMs)
	}
	if tel.CostPer1kTok <= 0 {
		t.Errorf("expected positive cost per 1k tokens, got %v", tel.CostPer1kTok)
	}
}

func TestRewriteVirtualModel(t *testing.T) {
	r, _ := setupTestRouter()
	body := []byte(`{"model":"smart/auto","messages":[{"role":"user","content":"hi"}]}`)
	model, updated, err := RewriteVirtualModel("smart/auto", body, r)
	if err != nil {
		t.Fatalf("rewrite failed: %v", err)
	}
	if model == "" || model == "smart/auto" {
		t.Errorf("expected concrete model, got %q", model)
	}
	if string(updated) == string(body) {
		t.Error("expected body to be rewritten")
	}
}

func TestRewriteVirtualModel_Passthrough(t *testing.T) {
	r, _ := setupTestRouter()
	body := []byte(`{"model":"openai/gpt-4o","messages":[]}`)
	model, updated, err := RewriteVirtualModel("openai/gpt-4o", body, r)
	if err != nil {
		t.Fatal(err)
	}
	if model != "openai/gpt-4o" {
		t.Errorf("model changed unexpectedly: %q", model)
	}
	if string(updated) != string(body) {
		t.Error("expected body unchanged")
	}
}
