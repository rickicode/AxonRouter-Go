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
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
)

func newDashboardTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "dashboard-test.db")
	database, err := sql.Open("sqlite", tmp)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

func TestDashboardStats_IncludesTodayAndSystemMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)

	database := newDashboardTestDB(t)
	store := connstate.NewStore()
	tracker := usage.NewTracker(database)
	defer tracker.Stop()

	now := time.Now()
	todayMs := now.Truncate(24 * time.Hour).Add(time.Hour).UnixMilli()
	if _, err := database.Exec(`INSERT INTO request_logs (id, timestamp, provider_type_id, model_id, modality, input_tokens, output_tokens, latency_ms, error_message, cost_usd, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"log-1", todayMs, "openai", "gpt-4o", "chat", 10, 20, 100, "", 0.05, todayMs); err != nil {
		t.Fatalf("insert log: %v", err)
	}

	h := NewDashboardHandler(database, store, tracker)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/dashboard/stats", nil)
	h.Stats(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	checks := []string{
		"requests_today", "tokens_today", "cost_today",
		"errors_today", "avg_latency_ms_today",
		"cpu_percent", "memory_percent", "disk_percent",
	}
	for _, key := range checks {
		if _, ok := resp[key]; !ok {
			t.Errorf("missing key %q in response", key)
		}
	}
	if resp["requests_today"].(float64) != 1 {
		t.Errorf("requests_today = %v, want 1", resp["requests_today"])
	}
	if resp["tokens_today"].(float64) != 30 {
		t.Errorf("tokens_today = %v, want 30", resp["tokens_today"])
	}
	if resp["errors_today"].(float64) != 0 {
		t.Errorf("errors_today = %v, want 0", resp["errors_today"])
	}
}
