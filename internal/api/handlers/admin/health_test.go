package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/usage"
	"github.com/rickicode/AxonRouter-Go/internal/version"
)

func newHealthTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "health-test.db")
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

func TestHealth_IncludesVersionInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	version.SetTestVersion("0.3.3")
	defer version.ClearTestVersion()

	database := newHealthTestDB(t)
	store := connstate.NewStore()
	tracker := usage.NewTracker(database)
	defer tracker.Stop()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"tag_name":"v0.3.4","published_at":"2026-07-14T00:00:00Z","html_url":"https://github.com/rickicode/AxonRouter-Go/releases/tag/v0.3.4"}`))
	}))
	defer server.Close()

	checker := version.NewCheckerWithURL(server.Client(), server.URL)
	defer checker.Stop()
	if err := checker.Refresh(); err != nil {
		t.Fatalf("checker.Refresh: %v", err)
	}

	h := NewHealthHandler(database, store, tracker, checker)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/health", nil)
	h.Health(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["latest_version"] != "0.3.4" {
		t.Errorf("latest_version = %v, want 0.3.4", resp["latest_version"])
	}
	if resp["update_available"] != true {
		t.Errorf("update_available = %v, want true", resp["update_available"])
	}
}

func TestHealth_CurrentVersion_NoUpdate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	version.SetTestVersion("0.3.3")
	defer version.ClearTestVersion()

	database := newHealthTestDB(t)
	store := connstate.NewStore()
	tracker := usage.NewTracker(database)
	defer tracker.Stop()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"tag_name":"v0.3.3","published_at":"2026-07-14T00:00:00Z","html_url":"https://github.com/rickicode/AxonRouter-Go/releases/tag/v0.3.3"}`))
	}))
	defer server.Close()

	checker := version.NewCheckerWithURL(server.Client(), server.URL)
	defer checker.Stop()
	if err := checker.Refresh(); err != nil {
		t.Fatalf("checker.Refresh: %v", err)
	}

	h := NewHealthHandler(database, store, tracker, checker)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/health", nil)
	h.Health(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["latest_version"] != "0.3.3" {
		t.Errorf("latest_version = %v, want 0.3.3", resp["latest_version"])
	}
	if resp["update_available"] != false {
		t.Errorf("update_available = %v, want false", resp["update_available"])
	}
}
