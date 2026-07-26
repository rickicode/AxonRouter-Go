package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/auth/cursor"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
)

func newCursorImportHandlerForTest(t *testing.T, database *sql.DB, discover cursor.DiscoverFunc, client *http.Client) *CursorImportHandler {
	t.Helper()
	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	h := NewCursorImportHandler(database, store, elig)
	if discover != nil {
		h.searchFn = discover
	}
	if client != nil {
		h.client = client
	}
	return h
}

func TestImportCursorToken_Success(t *testing.T) {
	database := newConnectionHandlerTestDB(t)
	gin.SetMode(gin.TestMode)

	accessToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ1IiwiZXhwIjo5OTk5OTk5OTk5fQ.sig"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/usage" && r.Header.Get("Authorization") == "Bearer "+accessToken {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"startOfMonth":"2026-07-01T00:00:00.000Z"}`))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer ts.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	oldURL := cursor.UpstreamUsageURL()
	cursor.SetUpstreamUsageURL(ts.URL + "/auth/usage")
	defer cursor.SetUpstreamUsageURL(oldURL)

	h := newCursorImportHandlerForTest(t, database, func(ctx context.Context, roots cursor.SearchRoots) (*cursor.DiscoveredAuth, error) {
		return &cursor.DiscoveredAuth{AccessToken: accessToken, RefreshToken: "rt", Email: "user@example.com", Source: "/tmp/state.vscdb"}, nil
	}, client)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/oauth/cursor/import", nil)
	h.ImportCursorToken(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "ready" {
		t.Errorf("status = %q, want ready", resp.Status)
	}
	if resp.Name != "user@example.com" {
		t.Errorf("name = %q, want user@example.com", resp.Name)
	}

	var authType string
	if err := database.QueryRow(`SELECT auth_type FROM connections WHERE id = ?`, resp.ID).Scan(&authType); err != nil {
		t.Fatalf("fetch connection: %v", err)
	}
	if authType != "oauth" {
		t.Errorf("auth_type = %q, want oauth", authType)
	}

	if cs := h.store.Get(resp.ID); cs == nil || cs.Status != connstate.StatusReady {
		t.Errorf("in-memory status not ready: %v", cs)
	}
}

func TestImportCursorToken_NotFound(t *testing.T) {
	database := newConnectionHandlerTestDB(t)
	gin.SetMode(gin.TestMode)

	h := newCursorImportHandlerForTest(t, database, func(ctx context.Context, roots cursor.SearchRoots) (*cursor.DiscoveredAuth, error) {
		return nil, &cursor.DiscoveryError{TriedPaths: []string{"/a", "/b"}, Message: "not found"}
	}, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/oauth/cursor/import", nil)
	h.ImportCursorToken(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestImportCursorToken_ValidationFails(t *testing.T) {
	database := newConnectionHandlerTestDB(t)
	gin.SetMode(gin.TestMode)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer ts.Close()

	oldURL := cursor.UpstreamUsageURL()
	cursor.SetUpstreamUsageURL(ts.URL + "/auth/usage")
	defer cursor.SetUpstreamUsageURL(oldURL)

	h := newCursorImportHandlerForTest(t, database, func(ctx context.Context, roots cursor.SearchRoots) (*cursor.DiscoveredAuth, error) {
		return &cursor.DiscoveredAuth{AccessToken: "bad", Email: "x@y.com", Source: "/tmp/state.vscdb"}, nil
	}, &http.Client{Timeout: 10 * time.Second})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/oauth/cursor/import", nil)
	h.ImportCursorToken(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", w.Code, w.Body.String())
	}
}
