package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/quota"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

func newQuotaTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "quota-test.db")
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

func TestSummary_IncludesResetAndSpent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := newQuotaTestDB(t)
	now := time.Now().UTC()
	nowSec := now.Unix()

	// Seed one cached quota with a future reset (relative to now so the test
	// stays robust regardless of the current date).
	futureReset := now.Add(48 * time.Hour).Format(time.RFC3339)
	quotasJSON := `[{"name":"5h","used":1,"total":10,"remaining_pct":90,"reset_at":"` + futureReset + `"}]`
	db.Exec(`INSERT INTO quota_cache (id, connection_id, provider_type_id, connection_name, plan, quotas, status, fetched_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"cx-1", "conn-cx", "cx", "Codex 1", "plus",
		quotasJSON,
		"ok", nowSec, nowSec)

	// Seed a request log with cache savings this month.
	db.Exec(`INSERT INTO request_logs (id, timestamp, provider_type_id, model_id, modality, input_tokens, output_tokens, reasoning_tokens, cached_tokens, cache_creation_tokens, cost_usd, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"log-1", now.UnixMilli(), "cx", "cx/gpt-4o", "chat", 1000, 0, 0, 1000, 0, 0.00125, nowSec)

	h := NewQuotaHandler(db, connstate.NewStore())
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/quota/summary", nil)
	h.Summary(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp struct {
		Providers []struct {
			ProviderID string  `json:"provider_id"`
			NextReset  string  `json:"next_reset"`
			SpentUSD   float64 `json:"spent_usd"`
		} `json:"providers"`
		SpentUSD float64 `json:"spent_usd"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	var found bool
	for _, p := range resp.Providers {
		if p.ProviderID == "cx" {
			found = true
			if p.NextReset == "" {
				t.Errorf("expected next_reset for cx")
			}
			if p.SpentUSD <= 0 {
				t.Errorf("expected positive spent_usd for cx, got %f", p.SpentUSD)
			}
			if resp.SpentUSD <= 0 {
				t.Errorf("expected positive total spent_usd, got %f", resp.SpentUSD)
			}
		}
	}
	if !found {
		t.Fatalf("cx provider not in summary: %s", w.Body.String())
	}
}

func TestResetQuota_HealsConnection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newQuotaTestDB(t)
	now := time.Now().Unix()

	// Seed an active OAuth connection stuck in quota_exhausted.
	db.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status,
		is_active, cooldown_until, last_error, last_error_code, failure_count,
		consecutive_ban_count, created_at, updated_at)
		VALUES (?, ?, ?, 'oauth', 'quota_exhausted', 1, ?, 'near limit', 'quota', 5, 2, ?, ?)`,
		"conn-reset-1", "cx", "Codex Test", now+3600, now, now)

	store := connstate.NewStore()
	store.SeedConnection("conn-reset-1", "cx", "quota_exhausted", 0)
	if cs := store.Get("conn-reset-1"); cs != nil {
		cs.SetQuotaCooldown(time.Now().Add(time.Hour))
		cs.GetModelLimit("cx/gpt-4o").SetCooldown(time.Now().Add(time.Hour))
	}

	exhaustion := quota.NewExhaustionCache()
	exhaustion.MarkExhausted("conn-reset-1", time.Hour)
	exhaustion.MarkExhausted(quota.ExhaustKey("conn-reset-1", "cx/gpt-4o"), time.Hour)

	elig := connstate.NewEligibilityManager(store)
	elig.RecomputeAll()

	h := NewQuotaHandlerWithStore(db, store, elig, exhaustion)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/quota/conn-reset-1/reset", nil)
	c.Params = []gin.Param{{Key: "connId", Value: "conn-reset-1"}}
	h.Reset(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Status       string   `json:"status"`
		ConnectionID string   `json:"connection_id"`
		Provider     string   `json:"provider_type"`
		Models       []string `json:"models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" || resp.ConnectionID != "conn-reset-1" || resp.Provider != "cx" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	wantModels := map[string]bool{"cx/gpt-4o": true}
	gotModels := make(map[string]bool, len(resp.Models))
	for _, m := range resp.Models {
		gotModels[m] = true
	}
	for m := range wantModels {
		if !gotModels[m] {
			t.Errorf("expected model %q in response, got %v", m, resp.Models)
		}
	}

	// DB row should reflect the reset.
	var dbStatus string
	var cooldown sql.NullInt64
	db.QueryRow(`SELECT status, cooldown_until FROM connections WHERE id = ?`, "conn-reset-1").Scan(&dbStatus, &cooldown)
	if dbStatus != "ready" {
		t.Errorf("db status = %q, want ready", dbStatus)
	}
	if cooldown.Valid {
		t.Errorf("cooldown should be cleared, got %v", cooldown.Int64)
	}

	// In-memory state should be ready and exhaustion/cooldowns cleared.
	cs := store.Get("conn-reset-1")
	if cs == nil || cs.GetStatus() != connstate.StatusReady {
		t.Errorf("store status = %v, want ready", cs)
	}
	if exhaustion.IsExhausted("conn-reset-1") {
		t.Error("connection-wide exhaustion should be cleared")
	}
	if exhaustion.IsExhaustedScope("conn-reset-1", "cx/gpt-4o") {
		t.Error("scoped exhaustion should be cleared")
	}
	if cs.IsModelInCooldown("cx/gpt-4o") {
		t.Error("model cooldown should be cleared")
	}
}

func TestResetQuota_ReturnsNotFoundForMissingConnection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newQuotaTestDB(t)
	h := NewQuotaHandlerWithStore(db, connstate.NewStore(), nil, nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/quota/missing/reset", nil)
	c.Params = []gin.Param{{Key: "connId", Value: "missing"}}
	h.Reset(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestResetQuota_RejectsTerminalState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newQuotaTestDB(t)
	now := time.Now().Unix()
	db.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status,
		is_active, disabled_reason, created_at, updated_at)
		VALUES (?, ?, ?, 'oauth', 'disabled', 0, 'auth_failed', ?, ?)`,
		"conn-disabled", "cx", "Codex Disabled", now, now)

	h := NewQuotaHandlerWithStore(db, connstate.NewStore(), nil, nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/quota/conn-disabled/reset", nil)
	c.Params = []gin.Param{{Key: "connId", Value: "conn-disabled"}}
	h.Reset(c)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for terminal state, got %d body=%s", w.Code, w.Body.String())
	}
}
