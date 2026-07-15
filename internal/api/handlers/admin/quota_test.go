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

	"github.com/rickicode/AxonRouter-Go/internal/db"
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

func TestSummary_IncludesResetAndSavings(t *testing.T) {
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

	h := NewQuotaHandler(db)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/quota/summary", nil)
	h.Summary(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp struct {
		Providers []struct {
			ProviderID string `json:"provider_id"`
			NextReset  string `json:"next_reset"`
			SavingsUSD float64 `json:"savings_usd"`
		} `json:"providers"`
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
			if p.SavingsUSD <= 0 {
				t.Errorf("expected positive savings_usd for cx, got %f", p.SavingsUSD)
			}
		}
	}
	if !found {
		t.Fatalf("cx provider not in summary: %s", w.Body.String())
	}
}
