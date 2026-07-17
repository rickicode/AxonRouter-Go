package admin

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	_ "modernc.org/sqlite"
)

func newLogHandlerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "logs-test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

func TestLogHandlerClearRejectsUnsupportedRetention(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newLogHandlerTestDB(t)
	h := NewLogHandler(database)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := bytes.NewBufferString(`{"older_than_days":14}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/logs/clear", body)
	c.Request.Header.Set("Content-Type", "application/json")
	h.Clear(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestLogHandlerClearDeletesOldRequestLogsOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newLogHandlerTestDB(t)
	now := time.Now()
	oldTimestamp := now.AddDate(0, 0, -31).UnixMilli()
	freshTimestamp := now.AddDate(0, 0, -2).UnixMilli()

	for _, row := range []struct {
		id        string
		timestamp int64
	}{
		{id: "old-log", timestamp: oldTimestamp},
		{id: "fresh-log", timestamp: freshTimestamp},
	} {
		_, err := database.Exec(`INSERT INTO request_logs (id, timestamp, modality, created_at) VALUES (?, ?, ?, ?)`, row.id, row.timestamp, "chat", row.timestamp)
		if err != nil {
			t.Fatalf("seed request_logs %s: %v", row.id, err)
		}
	}
	_, err := database.Exec(`INSERT INTO api_keys (id, key_hash, key_value, name, created_at) VALUES (?, ?, ?, ?, ?)`, "key-log-clear", "hash", "raw", "log clear", now.Unix())
	if err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	_, err = database.Exec(`INSERT INTO api_key_usage (api_key_id, total_tokens, updated_at) VALUES (?, ?, ?)`, "key-log-clear", 1234, now.Unix())
	if err != nil {
		t.Fatalf("seed api_key_usage: %v", err)
	}

	h := NewLogHandler(database)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := bytes.NewBufferString(`{"older_than_days":30}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/logs/clear", body)
	c.Request.Header.Set("Content-Type", "application/json")
	h.Clear(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Deleted int64 `json:"deleted"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (body: %s)", resp.Deleted, w.Body.String())
	}

	var requestLogCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM request_logs`).Scan(&requestLogCount); err != nil {
		t.Fatalf("count request_logs: %v", err)
	}
	if requestLogCount != 1 {
		t.Fatalf("request_logs count = %d, want 1", requestLogCount)
	}
	var remainingID string
	if err := database.QueryRow(`SELECT id FROM request_logs`).Scan(&remainingID); err != nil {
		t.Fatalf("query remaining request log: %v", err)
	}
	if remainingID != "fresh-log" {
		t.Fatalf("remaining request log = %q, want fresh-log", remainingID)
	}

	var totalTokens int64
	if err := database.QueryRow(`SELECT total_tokens FROM api_key_usage WHERE api_key_id = ?`, "key-log-clear").Scan(&totalTokens); err != nil {
		t.Fatalf("query api_key_usage: %v", err)
	}
	if totalTokens != 1234 {
		t.Fatalf("api_key_usage total_tokens = %d, want 1234", totalTokens)
	}
}
