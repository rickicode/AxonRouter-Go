package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	_ "modernc.org/sqlite"
)

func newOptimizationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "optimization-test.db")
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

func TestGetCompressionMetrics_AggregatesRows(t *testing.T) {
	gin.SetMode(gin.TestMode)

	database := newOptimizationTestDB(t)
	now := int64(1234567890)

	// Seed metrics for two compression modes.
	if _, err := database.Exec(`INSERT INTO compression_metrics
		(mode, requests, original_tokens, compressed_tokens, updated_at)
		VALUES (?, ?, ?, ?, ?)`, "lite", 10, 1000, 850, now); err != nil {
		t.Fatalf("seed lite metrics: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO compression_metrics
		(mode, requests, original_tokens, compressed_tokens, updated_at)
		VALUES (?, ?, ?, ?, ?)`, "standard", 5, 500, 400, now); err != nil {
		t.Fatalf("seed standard metrics: %v", err)
	}

	h := NewOptimizationHandler(database, nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/compression/metrics", nil)
	h.GetCompressionMetrics(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp["total_requests"].(float64) != 15 {
		t.Errorf("total_requests = %v, want 15", resp["total_requests"])
	}
	if resp["original_tokens"].(float64) != 1500 {
		t.Errorf("original_tokens = %v, want 1500", resp["original_tokens"])
	}
	if resp["compressed_tokens"].(float64) != 1250 {
		t.Errorf("compressed_tokens = %v, want 1250", resp["compressed_tokens"])
	}
	if resp["tokens_saved"].(float64) != 250 {
		t.Errorf("tokens_saved = %v, want 250", resp["tokens_saved"])
	}
	savings := resp["savings_percent"].(float64)
	if savings < 16.6 || savings > 16.7 {
		t.Errorf("savings_percent = %v, want ~16.67", savings)
	}

	modes, ok := resp["modes"].([]any)
	if !ok || len(modes) != 2 {
		t.Fatalf("expected 2 mode entries, got %v", modes)
	}
}
