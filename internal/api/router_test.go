package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/web"
	"golang.org/x/crypto/bcrypt"
	"strings"
)


func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "router-test.db")
	database, err := sql.Open("sqlite", tmp)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.SetMaxOpenConns(50)
	database.SetMaxIdleConns(25)
	if _, err := database.Exec("PRAGMA journal_mode=WAL"); err != nil {
		t.Fatalf("wal mode: %v", err)
	}
	if _, err := database.Exec("PRAGMA busy_timeout=5000"); err != nil {
		t.Fatalf("busy timeout: %v", err)
	}
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func newTestRouter(t *testing.T) (*Router, *httptest.Server) {
	t.Helper()
	database := openTestDB(t)

	logging.Init("text")

	router := New(Config{
		DB:               database,
		Port: "0",
		QuotaIntervalMin: 1,
		LogRetentionDays: 30,
		WebFS:            web.GetBuildFS(),
	})

	return router, httptest.NewServer(router.Engine())
}

func TestHealth(t *testing.T) {
	_, srv := newTestRouter(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/admin/health")
	if err != nil {
		t.Fatalf("health request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" || body["db"] != "ok" {
		t.Errorf("unexpected health body: %+v", body)
	}
}

// loginForToken obtains a dashboard session JWT from the public login endpoint.
func loginForToken(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/admin/login", strings.NewReader(`{"password":"12345677"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	tok := resp.Header.Get("X-Auth-Token")
	if tok == "" {
		t.Fatalf("login response missing X-Auth-Token")
	}
	return tok
}

// TestAdminEndpointRequiresJWT proves that /api/admin routes reject
// unauthenticated requests and accept a valid session JWT.
func TestAdminEndpointRequiresJWT(t *testing.T) {
	_, srv := newTestRouter(t)
	defer srv.Close()

	// Unauthenticated → 401.
	resp, err := http.Get(srv.URL + "/api/admin/metrics")
	if err != nil {
		t.Fatalf("metrics request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	// Valid JWT → 200.
	tok := loginForToken(t, srv)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/admin/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	okResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authenticated metrics request: %v", err)
	}
	defer okResp.Body.Close()
	if okResp.StatusCode != http.StatusOK {
		t.Errorf("authenticated status = %d, want %d", okResp.StatusCode, http.StatusOK)
	}
}

func TestModelsEndpoint(t *testing.T) {
	router, srv := newTestRouter(t)
	defer srv.Close()
	defer router.Shutdown()

	// Seed a bcrypt-hashed API key so the endpoint is reachable.
	hash, _ := bcrypt.GenerateFromPassword([]byte("test-key"), bcrypt.DefaultCost)
	if _, err := router.db.Exec(
		`INSERT INTO api_keys (id, name, key_hash, is_active, rate_limit_per_min, created_at) VALUES (?, ?, ?, 1, 100, ?)`,
		"router-key-1", "test", string(hash), 0,
	); err != nil {
		t.Fatalf("insert api key: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://localhost/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rr := httptest.NewRecorder()
	router.Engine().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode models: %v", err)
	}
	if _, ok := body["data"]; !ok {
		t.Errorf("expected data field in models response, got %+v", body)
	}
}
