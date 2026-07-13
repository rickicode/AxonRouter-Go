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
)

// TestUsageHandlerReturnsRecordedRequests guards against a timestamp-unit
// regression: request_logs.timestamp is stored in MILLISECONDS
// (tracker.go sets time.Now().UnixMilli()), but parseFilters must compare
// against millisecond bounds. A previous bug returned seconds, making
// `rl.timestamp <= to_seconds` always false and the entire Usage page read 0.
func TestUsageHandlerReturnsRecordedRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	database, err := sql.Open("sqlite", filepath.Join(dir, "usage_test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Insert a row exactly as the tracker would (millisecond timestamp).
	ts := time.Now().UnixMilli()
	if _, err := database.Exec(
		`INSERT INTO request_logs
		 (id, timestamp, connection_id, provider_type_id, model_id, api_key_id,
		  modality, input_tokens, output_tokens, status_code, cost_usd, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"u1", ts, "conn-1", "openai", "gpt-4o", "key-1",
		"chat", 10, 20, 200, 0.01, ts,
	); err != nil {
		t.Fatalf("insert: %v", err)
	}

	h := NewUsageHandler(database)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/admin/usage", nil)
	h.Get(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			Summary struct {
				Requests int64 `json:"requests"`
			} `json:"summary"`
			ByTime     []map[string]any `json:"by_time"`
			ByProvider []map[string]any `json:"by_provider"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Data.Summary.Requests != 1 {
		t.Fatalf("expected 1 recorded request, got %d (body: %s)",
			resp.Data.Summary.Requests, w.Body.String())
	}
	if len(resp.Data.ByTime) == 0 {
		t.Fatalf("expected non-empty by_time buckets, got none")
	}
	if len(resp.Data.ByProvider) == 0 {
		t.Fatalf("expected non-empty by_provider breakdown, got none")
	}
}

// TestParseFiltersUsesMilliseconds pins the unit contract: From/To must be
// millisecond epochs so they compare directly against request_logs.timestamp.
func TestParseFiltersUsesMilliseconds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/admin/usage", nil)

	f := parseFilters(c)
	nowMs := time.Now().UnixMilli()
	if f.From > nowMs || f.To > nowMs+1000 {
		t.Fatalf("parseFilters returned non-millisecond bounds: From=%d To=%d (nowMs~%d)", f.From, f.To, nowMs)
	}
	// A second-resolution value would be ~1000x smaller than a millisecond one.
	if f.To < 1_000_000_000_000 {
		t.Fatalf("parseFilters To looks like seconds, not ms: %d", f.To)
	}
}
