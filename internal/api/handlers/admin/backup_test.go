package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/backup"
	appdb "github.com/rickicode/AxonRouter-Go/internal/db"

	_ "modernc.org/sqlite"
)

func TestBackupHandlerDownloadStreamsSelectedCategories(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newBackupHandlerTestDB(t)
	seedBackupHandlerTestData(t, database)
	h := NewBackupHandler(database, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/backup/download?categories=core,combos", nil)
	h.Download(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("content-type = %q, want application/x-ndjson", got)
	}
	if got := w.Header().Get("Transfer-Encoding"); got != "chunked" {
		t.Fatalf("transfer-encoding header = %q, want chunked", got)
	}
	if got := w.Header().Get("Content-Disposition"); !strings.Contains(got, "axonrouter-backup.ndjson") {
		t.Fatalf("content-disposition = %q, want backup filename", got)
	}

	lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected header and at least one row, got %q", w.Body.String())
	}
	var header backup.Header
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if strings.Join(header.Categories, ",") != "combos,core" {
		t.Fatalf("categories = %v, want sorted selected categories", header.Categories)
	}

	var sawProvider, sawCombo bool
	for _, line := range lines[1:] {
		var row backup.Row
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("decode row %q: %v", line, err)
		}
		sawProvider = sawProvider || row.Table == "provider_types"
		sawCombo = sawCombo || row.Table == "combos"
	}
	if !sawProvider || !sawCombo {
		t.Fatalf("expected provider_types and combos rows in %q", w.Body.String())
	}
}

func TestBackupHandlerDownloadRejectsUnknownCategory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewBackupHandler(newBackupHandlerTestDB(t), nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/backup/download?categories=missing", nil)
	h.Download(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestBackupHandlerRestoreMultipartCurrentTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	source := newBackupHandlerTestDB(t)
	seedBackupHandlerTestData(t, source)

	var backupPayload bytes.Buffer
	if err := backup.NewScanner(source).Backup(context.Background(), &backupPayload, []string{"core"}, ""); err != nil {
		t.Fatalf("create backup payload: %v", err)
	}

	target := newBackupHandlerTestDB(t)
	queue := appdb.NewWriteQueue(target)
	t.Cleanup(queue.Stop)
	h := NewBackupHandler(target, queue)

	body, contentType := multipartRestoreBody(t, "backup", "backup.ndjson", backupPayload.Bytes(), map[string]string{"target": "current"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/backup/restore", body)
	c.Request.Header.Set("Content-Type", contentType)
	h.Restore(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data backup.RestoreResult `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Data.RestartRequired || resp.Data.RowsRestored == 0 {
		t.Fatalf("unexpected restore result: %+v", resp.Data)
	}
	if got := countBackupHandlerRows(t, target, "provider_types"); got == 0 {
		t.Fatal("expected restored provider_types rows")
	}
}

func TestBackupHandlerRestoreRequiresBackupFile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewBackupHandler(newBackupHandlerTestDB(t), nil)

	body, contentType := multipartRestoreBody(t, "other", "backup.ndjson", []byte("not used"), map[string]string{"target": "current"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/backup/restore", body)
	c.Request.Header.Set("Content-Type", contentType)
	h.Restore(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func newBackupHandlerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "backup-handler.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := appdb.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return database
}

func seedBackupHandlerTestData(t *testing.T, d *sql.DB) {
	t.Helper()
	now := int64(1700000000)
	mustBackupHandlerExec(t, d, `INSERT OR REPLACE INTO provider_types (id, display_name, format, base_url, is_custom, category, service_kinds, created_at) VALUES ('handler-provider', 'Handler Provider', 'openai', 'https://example.test/v1', 1, 'apikey', '["llm"]', ?)`, now)
	mustBackupHandlerExec(t, d, `INSERT OR REPLACE INTO connections (id, provider_type_id, name, auth_type, api_key, status, is_active, created_at, updated_at) VALUES ('handler-conn', 'handler-provider', 'Handler Conn', 'api_key', 'secret', 'ready', 1, ?, ?)`, now, now)
	mustBackupHandlerExec(t, d, `INSERT OR REPLACE INTO api_keys (id, key_hash, name, rate_limit_per_min, is_active, created_at) VALUES ('handler-key', 'hash-handler', 'Handler Key', 60, 1, ?)`, now)
	mustBackupHandlerExec(t, d, `INSERT OR REPLACE INTO combos (id, name, strategy, created_at, updated_at) VALUES ('handler-combo', 'Handler Combo', 'priority', ?, ?)`, now, now)
	mustBackupHandlerExec(t, d, `INSERT OR REPLACE INTO combo_steps (id, combo_id, connection_id, model_id, priority, created_at) VALUES ('handler-step', 'handler-combo', 'handler-conn', 'gpt-test', 1, ?)`, now)
}

func multipartRestoreBody(t *testing.T, fieldName, fileName string, fileContent []byte, fields map[string]string) (io.Reader, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	part, err := writer.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(fileContent); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &body, writer.FormDataContentType()
}

func mustBackupHandlerExec(t *testing.T, d *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := d.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func countBackupHandlerRows(t *testing.T, d *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := d.QueryRow("SELECT COUNT(*) FROM \"" + table + "\"").Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}
