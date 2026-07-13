package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/rickicode/AxonRouter-Go/internal/connstate"

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

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" || body["db"] != "ok" {
		t.Errorf("unexpected health body: %+v", body)
	}
	if _, ok := body["must_change_password"]; !ok {
		t.Errorf("expected must_change_password in health response")
	}
}

// loginForToken obtains a dashboard session JWT from the public login endpoint.
func loginForToken(t *testing.T, srv *httptest.Server, r *Router) string {
	t.Helper()
	var password string
	if err := r.db.QueryRow(`SELECT value FROM settings WHERE key = 'admin_password_plain'`).Scan(&password); err != nil {
		t.Fatalf("read initial password: %v", err)
	}
	body := `{"password":"` + password + `"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/admin/login", strings.NewReader(body))
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
	router, srv := newTestRouter(t)
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

	// Mark password as changed so SessionAuth allows non-password endpoints.
	_ = setSetting(router.db, firstLoginKey, "false")

	// Valid JWT → 200.
	tok := loginForToken(t, srv, router)
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

// TestSeedConnectionsFromDB_RestoresCooldown proves that a connection with an
// active cooldown_until in the DB is not considered eligible right after startup.
func TestRoutesRegistered_DeadHandlersExist(t *testing.T) {
	router, srv := newTestRouter(t)
	defer srv.Close()
	defer router.Shutdown()

	hash, _ := bcrypt.GenerateFromPassword([]byte("test-key"), bcrypt.DefaultCost)
	if _, err := router.db.Exec(
		`INSERT INTO api_keys (id, name, key_hash, is_active, rate_limit_per_min, created_at) VALUES (?, ?, ?, 1, 100, ?)`,
		"router-key-dead", "test", string(hash), 0,
	); err != nil {
		t.Fatalf("insert api key: %v", err)
	}

	for _, path := range []string{"/v1/embeddings", "/v1/responses"} {
		req, _ := http.NewRequest(http.MethodPost, "http://localhost"+path, strings.NewReader(`{"model":"openai/test"}`))
		req.Header.Set("Authorization", "Bearer test-key")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		router.Engine().ServeHTTP(rr, req)

		if rr.Code == http.StatusNotFound {
			t.Errorf("%s returned 404; route should be registered", path)
		}
	}
}

func TestSeedConnectionsFromDB_RestoresCooldown(t *testing.T) {
	database := openTestDB(t)
	now := time.Now().Unix()
	future := now + 3600 // 1 hour from now

	if _, err := database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, cooldown_until, is_active, created_at, updated_at) VALUES ('conn-oc-1','oc','prox8','none','rate_limited',?,1,?,?)`, future, now, now); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	store := connstate.NewStore()
	seedConnectionsFromDB(database, store)

	cs := store.Get("conn-oc-1")
	if cs == nil {
		t.Fatal("connection not seeded")
	}
	if cs.Status != connstate.StatusCooldown {
		// SetCooldown stores status as "cooldown" in memory, while DB keeps "rate_limited".
		t.Fatalf("status = %q, want cooldown", cs.Status)
	}
	if !cs.IsInCooldown() {
		t.Fatal("expected connection to be in cooldown after seed")
	}
}

// TestSeedConnectionsFromDB_ExpiresStaleCooldown proves that a connection whose
// cooldown_until is in the past is treated as ready on startup.
func TestSeedConnectionsFromDB_ExpiresStaleCooldown(t *testing.T) {
	database := openTestDB(t)
	now := time.Now().Unix()
	past := now - 3600 // 1 hour ago

	if _, err := database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, cooldown_until, is_active, created_at, updated_at) VALUES ('conn-oc-2','oc','prox9','none','rate_limited',?,1,?,?)`, past, now, now); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	store := connstate.NewStore()
	seedConnectionsFromDB(database, store)

	cs := store.Get("conn-oc-2")
	if cs == nil {
		t.Fatal("connection not seeded")
	}
	if cs.Status != connstate.StatusReady {
		t.Fatalf("status = %q, want ready (stale cooldown should be ignored)", cs.Status)
	}
	if cs.IsInCooldown() {
		t.Fatal("expected connection to be ready after expired cooldown")
	}
}
