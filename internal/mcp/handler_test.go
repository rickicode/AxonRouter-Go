package mcp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"
)

func handlerTestDB(t *testing.T) (*sql.DB, *Handler) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS mcp_servers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			command TEXT NOT NULL,
			args TEXT NOT NULL DEFAULT '[]',
			env TEXT NOT NULL DEFAULT '{}',
			enabled INTEGER NOT NULL DEFAULT 1,
			restart_policy TEXT NOT NULL DEFAULT 'on-failure',
			max_clients INTEGER NOT NULL DEFAULT 4,
			max_idle_sec INTEGER NOT NULL DEFAULT 60,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	h := NewHandler(db)
	t.Cleanup(func() {
		h.Stop(context.Background())
		db.Close()
	})
	return db, h
}

func TestHandlerAdminCRUD(t *testing.T) {
	_, h := handlerTestDB(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/admin/mcp", h.ListAdmin)
	r.POST("/api/admin/mcp", h.CreateAdmin)
	r.PATCH("/api/admin/mcp/:id", h.UpdateAdmin)
	r.DELETE("/api/admin/mcp/:id", h.DeleteAdmin)

	createBody := `{"name":"echo","command":"echo","args":["hello"],"enabled":true,"restart_policy":"never"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/admin/mcp", bytes.NewBufferString(createBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", w.Code, w.Body.String())
	}
	var created struct {
		Data adminServerView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created: %v", err)
	}
	id := created.Data.ID
	if id == "" {
		t.Fatal("expected id")
	}

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/admin/mcp", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status %d: %s", w.Code, w.Body.String())
	}
	var list struct {
		Data []adminServerView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(list.Data) != 1 {
		t.Fatalf("expected 1 server, got %d", len(list.Data))
	}

	patch := `{"name":"echo2"}`
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PATCH", "/api/admin/mcp/"+id, bytes.NewBufferString(patch))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update status %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/api/admin/mcp/"+id, nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status %d: %s", w.Code, w.Body.String())
	}
}

func TestHandlerCreateRejectsInjection(t *testing.T) {
	_, h := handlerTestDB(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/admin/mcp", h.CreateAdmin)

	body := `{"name":"bad","command":"node; rm -rf /","args":[],"enabled":true}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/admin/mcp", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
