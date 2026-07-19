package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

"github.com/gin-gonic/gin"
"github.com/rickicode/AxonRouter-Go/internal/auth"
"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/db"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
)

// TestAddConnectionMimocode validates MiMoCode connection creation rules:
// additional connections require a proxy pool, get forced to no-auth, and
// receive auto-generated account/fingerprint fields.
func TestAddConnectionMimocode(t *testing.T) {
	database := newConnectionHandlerTestDB(t)
	h := newProviderHandlerTestDeps(t, database)

	now := time.Now().Unix()
	// Seed an active proxy pool to use for the valid creation path.
	if _, err := database.Exec(`INSERT INTO proxy_pools (id, name, type, is_active, created_at, updated_at) VALUES ('pool-1','Pool One','http',1,?,?)`, now, now); err != nil {
		t.Fatalf("seed proxy pool: %v", err)
	}

	gin.SetMode(gin.TestMode)

	// 1. Missing proxy pool must return 400.
	w1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w1)
	body1 := `{"name":"MiMoCode No Proxy","provider_specific_data":{}}`
	c1.Request = httptest.NewRequest(http.MethodPost, "/api/admin/providers/mimocode/connections", strings.NewReader(body1))
	c1.Request.Header.Set("Content-Type", "application/json")
	c1.Params = []gin.Param{{Key: "id", Value: "mimocode"}}
	h.AddConnection(c1)
	if w1.Code != http.StatusBadRequest {
		t.Fatalf("missing proxy pool: status = %d, want 400; body=%s", w1.Code, w1.Body.String())
	}

	// 2. With proxy pool must return 201 and store no-auth settings.
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	body2 := `{"name":"MiMoCode With Proxy","provider_specific_data":{"proxyPoolId":"pool-1"}}`
	c2.Request = httptest.NewRequest(http.MethodPost, "/api/admin/providers/mimocode/connections", strings.NewReader(body2))
	c2.Request.Header.Set("Content-Type", "application/json")
	c2.Params = []gin.Param{{Key: "id", Value: "mimocode"}}
	h.AddConnection(c2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("with proxy pool: status = %d, want 201; body=%s", w2.Code, w2.Body.String())
	}
	var resp struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Name != "MiMoCode With Proxy" {
		t.Errorf("name = %q, want MiMoCode With Proxy", resp.Name)
	}

	var authType, apiKey, psdRaw string
	err := database.QueryRow(`SELECT auth_type, COALESCE(api_key,''), COALESCE(provider_specific_data,'') FROM connections WHERE id = ?`, resp.ID).Scan(&authType, &apiKey, &psdRaw)
	if err != nil {
		t.Fatalf("fetch created connection: %v", err)
	}
	if authType != "none" {
		t.Errorf("auth_type = %q, want none", authType)
	}
	if apiKey != "" {
		t.Errorf("api_key = %q, want empty", apiKey)
	}
	var psd map[string]string
	if err := json.Unmarshal([]byte(psdRaw), &psd); err != nil {
		t.Fatalf("psd unmarshal: %v", err)
	}
	if psd["proxyPoolId"] != "pool-1" {
		t.Errorf("proxyPoolId = %q, want pool-1", psd["proxyPoolId"])
	}
	if psd["accountId"] == "" {
		t.Errorf("accountId not generated")
	}
	if psd["accountLabel"] != "MiMoCode With Proxy" {
		t.Errorf("accountLabel = %q, want MiMoCode With Proxy", psd["accountLabel"])
	}
	if len(psd["fingerprint"]) != 64 {
		t.Errorf("fingerprint length = %d, want 64", len(psd["fingerprint"]))
	}
}

// countingMockExecutor counts the maximum number of concurrent ExecuteStream calls.
type countingMockExecutor struct {
	mu        sync.Mutex
	active    int
	maxActive int
	delay     time.Duration
}

func (m *countingMockExecutor) Execute(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	return nil, errors.New("not implemented")
}

func (m *countingMockExecutor) ExecuteStream(ctx context.Context, req *executor.Request) (*executor.StreamResult, error) {
	m.mu.Lock()
	m.active++
	if m.active > m.maxActive {
		m.maxActive = m.active
	}
	m.mu.Unlock()

	time.Sleep(m.delay)

	m.mu.Lock()
	m.active--
	m.mu.Unlock()

	ch := make(chan executor.StreamChunk, 1)
	ch <- executor.StreamChunk{Payload: []byte(`{"ok":true}`)}
	close(ch)
	return &executor.StreamResult{Chunks: ch}, nil
}

func (m *countingMockExecutor) MaxActive() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.maxActive
}

// newProviderHandlerTestDeps creates the common dependencies needed for provider
// handler tests that don't exercise connection testing.
func newProviderHandlerTestDeps(t *testing.T, database *sql.DB) *ProviderHandler {
	t.Helper()
	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	providerCfg := providercfg.NewManager("")
	return NewProviderHandler(database, executor.GetRegistry(), store, elig, providerCfg, nil, nil)
}

// TestAll_BatchesToMaxTen proves that TestAll never runs more than 10 connection
// tests concurrently, even when a provider has many connections.
func TestAll_BatchesToMaxTen(t *testing.T) {
	database := newConnectionHandlerTestDB(t)

	now := time.Now().Unix()
	if _, err := database.Exec(`INSERT INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('batchp','Batch Provider','openai','http://x',?)`, now); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}

	const totalConns = 25
	for i := range totalConns {
		id := "conn-" + string(rune('a'+i))
		if _, err := database.Exec(`
			INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at)
			VALUES (?,'batchp',?,'none','ready',1,?,?)
		`, id, id, now, now); err != nil {
			t.Fatalf("seed connection %d: %v", i, err)
		}
	}

	mock := &countingMockExecutor{delay: 50 * time.Millisecond}
	registry := executor.GetRegistry()
	registry.Register("batchp", executor.FormatOpenAI, mock)

	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	providerCfg := providercfg.NewManager("")
	h := NewProviderHandler(database, registry, store, elig, providerCfg, nil, nil)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/providers/batchp/test", nil)
	c.Params = []gin.Param{{Key: "id", Value: "batchp"}}

	h.TestAll(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Count(body, `"status":"ok"`) != totalConns {
		t.Fatalf("expected %d ok results, body=%s", totalConns, body)
	}
	if mock.MaxActive() > testAllBatchSize {
		t.Fatalf("max concurrent = %d, want <= %d", mock.MaxActive(), testAllBatchSize)
	}
	if mock.MaxActive() == 0 {
		t.Fatalf("mock was never called concurrently")
	}
}

// tokenCapturingExecutor captures the access token used on the last ExecuteStream
// call and returns a trivial successful stream.
type tokenCapturingExecutor struct {
	lastAccessToken string
}

func (m *tokenCapturingExecutor) Execute(ctx context.Context, req *executor.Request) (*executor.Response, error) {
	return nil, errors.New("not implemented")
}

func (m *tokenCapturingExecutor) ExecuteStream(ctx context.Context, req *executor.Request) (*executor.StreamResult, error) {
	m.lastAccessToken = req.AccessToken
	ch := make(chan executor.StreamChunk, 1)
	ch <- executor.StreamChunk{Payload: []byte(`{"ok":true}`)}
	close(ch)
	return &executor.StreamResult{Chunks: ch}, nil
}

// fakeOAuthService is a test double for auth.OAuthService.
type fakeOAuthService struct {
	refreshFunc func(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error)
}

func (f *fakeOAuthService) GenerateAuthURL(ctx context.Context, state string) (string, error) {
	return "", nil
}

func (f *fakeOAuthService) ExchangeCode(ctx context.Context, code string) (*auth.Credentials, error) {
	return nil, nil
}

func (f *fakeOAuthService) RefreshToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	return f.refreshFunc(ctx, creds)
}

func (f *fakeOAuthService) StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error) {
	return 0, nil, nil
}

// TestAll_RefreshesNearExpiryOAuth proves TestAll refreshes an OAuth token that is
// within the provider's lead time and tests with the new token.
func TestAll_RefreshesNearExpiryOAuth(t *testing.T) {
	database := newConnectionHandlerTestDB(t)
	writeQueue := db.NewWriteQueue(database)
	t.Cleanup(writeQueue.Stop)

	now := time.Now().Unix()
	expiresAt := time.Now().Add(2 * time.Minute).Unix()
	if _, err := database.Exec(`INSERT INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('testp','Test Provider','openai','http://x',?)`, now); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO connections (id, provider_type_id, name, auth_type, oauth_token, oauth_refresh_token, oauth_expires_at, status, is_active, created_at, updated_at)
		VALUES ('conn-test','testp','Test Conn','oauth','old-access','old-refresh',?,'ready',1,?,?)
	`, expiresAt, now, now); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	authMgr := auth.NewManager()
	authMgr.RegisterService(auth.ProviderType("testp"), &fakeOAuthService{
		refreshFunc: func(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
			return &auth.Credentials{
				AccessToken:  "new-access",
				RefreshToken: "new-refresh",
				ExpiresAt:    time.Now().Add(1 * time.Hour),
			}, nil
		},
	})

	mock := &tokenCapturingExecutor{}
	registry := executor.GetRegistry()
	registry.Register("testp", executor.FormatOpenAI, mock)

	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	providerCfg := providercfg.NewManager("")
	h := NewProviderHandler(database, registry, store, elig, providerCfg, writeQueue, authMgr)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/providers/testp/test", nil)
	c.Params = []gin.Param{{Key: "id", Value: "testp"}}

	h.TestAll(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"status":"ok"`) {
		t.Fatalf("expected ok result, body=%s", body)
	}
	if mock.lastAccessToken != "new-access" {
		t.Fatalf("executor called with access token %q, want new-access", mock.lastAccessToken)
	}
	var persistedToken string
	if err := database.QueryRow(`SELECT COALESCE(oauth_token,'') FROM connections WHERE id = ?`, "conn-test").Scan(&persistedToken); err != nil {
		t.Fatalf("fetch persisted token: %v", err)
	}
	if persistedToken != "new-access" {
		t.Fatalf("persisted token = %q, want new-access", persistedToken)
	}
}

// TestAll_MarksAuthFailedOnUnrecoverableRefreshError proves TestAll marks a
// connection auth_failed when token refresh fails with an unrecoverable error.
func TestAll_MarksAuthFailedOnUnrecoverableRefreshError(t *testing.T) {
	database := newConnectionHandlerTestDB(t)
	writeQueue := db.NewWriteQueue(database)
	t.Cleanup(writeQueue.Stop)

	now := time.Now().Unix()
	expiresAt := time.Now().Add(2 * time.Minute).Unix()
	if _, err := database.Exec(`INSERT INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('testp','Test Provider','openai','http://x',?)`, now); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO connections (id, provider_type_id, name, auth_type, oauth_token, oauth_refresh_token, oauth_expires_at, status, is_active, created_at, updated_at)
		VALUES ('conn-bad','testp','Bad Conn','oauth','old-access','old-refresh',?,'ready',1,?,?)
	`, expiresAt, now, now); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	authMgr := auth.NewManager()
	authMgr.RegisterService(auth.ProviderType("testp"), &fakeOAuthService{
		refreshFunc: func(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
			return nil, errors.New("invalid_grant")
		},
	})

	mock := &tokenCapturingExecutor{}
	registry := executor.GetRegistry()
	registry.Register("testp", executor.FormatOpenAI, mock)

	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	providerCfg := providercfg.NewManager("")
	h := NewProviderHandler(database, registry, store, elig, providerCfg, writeQueue, authMgr)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/providers/testp/test", nil)
	c.Params = []gin.Param{{Key: "id", Value: "testp"}}

	h.TestAll(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"status":"failed"`) {
		t.Fatalf("expected failed result, body=%s", body)
	}
	if mock.lastAccessToken != "" {
		t.Fatalf("executor should not have been called, got access token %q", mock.lastAccessToken)
	}

	var isActive int
	var status string
	if err := database.QueryRow(`SELECT is_active, status FROM connections WHERE id = ?`, "conn-bad").Scan(&isActive, &status); err != nil {
		t.Fatalf("fetch connection status: %v", err)
	}
	if isActive != 0 {
		t.Fatalf("is_active = %d, want 0", isActive)
	}
	if status != "auth_failed" {
		t.Fatalf("status = %q, want auth_failed", status)
	}
}

// TestTestAllRefreshLead_IncludesGrokCli proves the admin TestAll refresh lead map
// has an explicit 5-minute entry for grok-cli so OAuth tokens are refreshed before expiry.
func TestTestAllRefreshLead_IncludesGrokCli(t *testing.T) {
	if lead, ok := testAllRefreshLead["grok-cli"]; !ok || lead != 5*time.Minute {
		t.Fatalf("testAllRefreshLead missing or invalid grok-cli entry: %v, ok=%v", lead, ok)
	}
}

// TestProviderList_IncludesCategoryAndServiceKinds proves every provider returned
// by the admin list endpoint carries category and a non-empty service_kinds array.
func TestProviderList_IncludesCategoryAndServiceKinds(t *testing.T) {
	database := newConnectionHandlerTestDB(t)
	h := newProviderHandlerTestDeps(t, database)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/providers", nil)

	h.List(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Data []struct {
			ID           string   `json:"id"`
			Category     string   `json:"category"`
			ServiceKinds []string `json:"service_kinds"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Data) == 0 {
		t.Fatalf("expected seeded providers, got none")
	}
	for _, p := range resp.Data {
		if p.Category == "" {
			t.Errorf("provider %q missing category", p.ID)
		}
		if len(p.ServiceKinds) == 0 {
			t.Errorf("provider %q missing service_kinds", p.ID)
		}
	}

	// Spot-check the multi-service provider seeded by migrations.
	var cfFound bool
	for _, p := range resp.Data {
		if p.ID == "cf" {
			cfFound = true
			want := []string{"llm", "embedding", "image"}
			if !slicesEqual(p.ServiceKinds, want) {
				t.Errorf("cf service_kinds = %v, want %v", p.ServiceKinds, want)
			}
		}
	}
	if !cfFound {
		t.Errorf("seeded provider 'cf' not found in list")
	}
}

// TestProviderGet_IncludesCategoryAndServiceKinds proves the admin get endpoint
// returns category and service_kinds for a provider.
func TestProviderGet_IncludesCategoryAndServiceKinds(t *testing.T) {
	database := newConnectionHandlerTestDB(t)
	h := newProviderHandlerTestDeps(t, database)

	now := time.Now().Unix()
	if _, err := database.Exec(`
		INSERT INTO provider_types (id, display_name, format, base_url, is_custom, category, service_kinds, created_at)
		VALUES ('customp','Custom Provider','openai','http://x',1,'no-auth','["llm","image"]',?)
	`, now); err != nil {
		t.Fatalf("seed provider_type: %v", err)
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/providers/customp", nil)
	c.Params = []gin.Param{{Key: "id", Value: "customp"}}

	h.Get(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	var p struct {
		ID           string   `json:"id"`
		Category     string   `json:"category"`
		ServiceKinds []string `json:"service_kinds"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if p.ID != "customp" {
		t.Fatalf("id = %q, want customp", p.ID)
	}
	if p.Category != "no-auth" {
		t.Errorf("category = %q, want no-auth", p.Category)
	}
	want := []string{"llm", "image"}
	if !slicesEqual(p.ServiceKinds, want) {
		t.Errorf("service_kinds = %v, want %v", p.ServiceKinds, want)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func seedProviderType(t *testing.T, database *sql.DB, id string) {
	t.Helper()
	now := time.Now().Unix()
	if _, err := database.Exec(
		`INSERT INTO provider_types (id, display_name, format, base_url, created_at) VALUES (?,?,?,?,?)`,
		id, id, "openai", "http://x", now,
	); err != nil {
		t.Fatalf("seed provider_type %s: %v", id, err)
	}
}

func bulkHandlerWithQueue(t *testing.T, database *sql.DB) *ProviderHandler {
	t.Helper()
	wq := db.NewWriteQueue(database)
	t.Cleanup(wq.Stop)
	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	return &ProviderHandler{db: database, store: store, elig: elig, writeQueue: wq}
}

func TestBulkAddConnections_LargeImport(t *testing.T) {
	database := newConnectionHandlerTestDB(t)
	seedProviderType(t, database, "bulktest")
	h := bulkHandlerWithQueue(t, database)

	gin.SetMode(gin.TestMode)
	const n = 1000
	conns := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		conns = append(conns, map[string]any{
			"name":    "conn-" + strconv.Itoa(i),
			"api_key": "sk-" + strconv.Itoa(i),
			"priority": 1,
		})
	}
	body, err := json.Marshal(map[string]any{"connections": conns})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/providers/bulktest/connections/bulk", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = []gin.Param{{Key: "id", Value: "bulktest"}}
	h.BulkAddConnections(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Created int      `json:"created"`
		Total   int      `json:"total"`
		Failed  int      `json:"failed"`
		Errors  []string `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Created != n || resp.Total != n || resp.Failed != 0 {
		t.Fatalf("created=%d total=%d failed=%d, want %d/%d/0 (errors=%v)", resp.Created, resp.Total, resp.Failed, n, n, resp.Errors)
	}
	var cnt int
	if err := database.QueryRow(`SELECT COUNT(*) FROM connections WHERE provider_type_id = 'bulktest'`).Scan(&cnt); err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != n {
		t.Fatalf("db rows=%d, want %d", cnt, n)
	}
}

func TestBulkAddConnections_PartialFailure(t *testing.T) {
	database := newConnectionHandlerTestDB(t)
	seedProviderType(t, database, "bulktest")
	h := bulkHandlerWithQueue(t, database)

	gin.SetMode(gin.TestMode)
	// Force a real per-row failure: a test-only unique index on name means the
	// second "dup" row collides while the others commit (no all-or-nothing loss).
	if _, err := database.Exec(`DELETE FROM connections`); err != nil {
		t.Fatalf("clear seeded connections: %v", err)
	}
	if _, err := database.Exec(`CREATE UNIQUE INDEX test_uniq_name ON connections(name)`); err != nil {
		t.Fatalf("create unique index: %v", err)
	}
	conns := []map[string]any{
		{"name": "ok-1", "api_key": "sk-1"},
		{"name": "dup", "api_key": "sk-dup"},
		{"name": "dup", "api_key": "sk-dup2"},
		{"name": "ok-2", "api_key": "sk-2"},
	}
	body, _ := json.Marshal(map[string]any{"connections": conns})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/providers/bulktest/connections/bulk", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = []gin.Param{{Key: "id", Value: "bulktest"}}
	h.BulkAddConnections(c)

	var resp struct {
		Created int      `json:"created"`
		Total   int      `json:"total"`
		Failed  int      `json:"failed"`
		Errors  []string `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Created != 3 || resp.Failed != 1 {
		t.Fatalf("created=%d failed=%d, want 3/1 (errors=%v)", resp.Created, resp.Failed, resp.Errors)
	}
	var cnt int
	if err := database.QueryRow(`SELECT COUNT(*) FROM connections WHERE provider_type_id = 'bulktest'`).Scan(&cnt); err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 3 {
		t.Fatalf("db rows=%d, want 3", cnt)
	}
}

func seedOCProviderAndConnections(t *testing.T, database *sql.DB) (string, string, string) {
	t.Helper()
	now := time.Now().Unix()
	if _, err := database.Exec(`DELETE FROM connections WHERE provider_type_id = 'oc'`); err != nil {
		t.Fatalf("clear oc connections: %v", err)
	}
	if _, err := database.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('oc','OpenCode Free','openai','http://x',?)`, now); err != nil {
		t.Fatalf("seed provider_type oc: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO proxy_pools (id, name, type, is_active, created_at, updated_at) VALUES ('pool-a','Pool A','http',1,?,?), ('pool-b','Pool B','http',1,?,?)`, now, now, now, now); err != nil {
		t.Fatalf("seed proxy pools: %v", err)
	}
	conn1 := "oc-conn-1"
	conn2 := "oc-conn-2"
	if _, err := database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, priority, provider_specific_data, is_active, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		conn1, "oc", "OC 1", "none", "ready", 1, `{"accountId":"a1"}`, 1, now, now); err != nil {
		t.Fatalf("seed connection 1: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, priority, provider_specific_data, is_active, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		conn2, "oc", "OC 2", "none", "ready", 2, `{"accountId":"a2","proxyPoolId":"pool-a"}`, 1, now, now); err != nil {
		t.Fatalf("seed connection 2: %v", err)
	}
	return conn1, conn2, "pool-b"
}

// TestBulkAssignProxy_BindAndUnbind updates provider_specific_data.proxyPoolId
// for multiple oc connections in a single transaction and supports unbinding.
func TestBulkAssignProxy_BindAndUnbind(t *testing.T) {
	database := newConnectionHandlerTestDB(t)
	conn1, conn2, poolB := seedOCProviderAndConnections(t, database)

	h := newProviderHandlerTestDeps(t, database)
	gin.SetMode(gin.TestMode)

	// 1. Bind both connections to pool-b.
	body, _ := json.Marshal(map[string]any{"connection_ids": []string{conn1, conn2}, "proxy_pool_id": poolB})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/providers/oc/connections/bulk-proxy", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = []gin.Param{{Key: "id", Value: "oc"}}
	h.BulkAssignProxy(c)

	if w.Code != http.StatusOK {
		t.Fatalf("bind status=%d, body=%s", w.Code, w.Body.String())
	}
	var bindResp struct {
		Updated int `json:"updated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &bindResp); err != nil {
		t.Fatalf("unmarshal bind resp: %v", err)
	}
	if bindResp.Updated != 2 {
		t.Fatalf("updated=%d, want 2", bindResp.Updated)
	}

	assertPool := func(connID, want string) {
		t.Helper()
		var raw string
		if err := database.QueryRow(`SELECT COALESCE(provider_specific_data,'') FROM connections WHERE id = ?`, connID).Scan(&raw); err != nil {
			t.Fatalf("fetch psd: %v", err)
		}
		var psd map[string]string
		if err := json.Unmarshal([]byte(raw), &psd); err != nil {
			t.Fatalf("unmarshal psd: %v", err)
		}
		if got := psd["proxyPoolId"]; got != want {
			t.Errorf("%s proxyPoolId=%q, want %q", connID, got, want)
		}
	}
	assertPool(conn1, "pool-b")
	assertPool(conn2, "pool-b")

	// 2. Unbind by sending null proxy_pool_id.
	body, _ = json.Marshal(map[string]any{"connection_ids": []string{conn1, conn2}, "proxy_pool_id": nil})
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/providers/oc/connections/bulk-proxy", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = []gin.Param{{Key: "id", Value: "oc"}}
	h.BulkAssignProxy(c)

	if w.Code != http.StatusOK {
		t.Fatalf("unbind status=%d, body=%s", w.Code, w.Body.String())
	}
	assertPool(conn1, "")
	assertPool(conn2, "")
}

// TestBulkAssignProxy_ProviderWithoutProxyPools rejects bulk proxy assignment
// for providers that do not require a proxy pool.
func TestBulkAssignProxy_ProviderWithoutProxyPools(t *testing.T) {
	database := newConnectionHandlerTestDB(t)
	now := time.Now().Unix()
	if _, err := database.Exec(`INSERT OR IGNORE INTO provider_types (id, display_name, format, base_url, created_at) VALUES ('openai','OpenAI','openai','http://x',?)`, now); err != nil {
		t.Fatalf("seed openai: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO connections (id, provider_type_id, name, auth_type, status, is_active, created_at, updated_at) VALUES ('oa-1','openai','OA 1','api_key','ready',1,?,?)`, now, now); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	h := newProviderHandlerTestDeps(t, database)
	gin.SetMode(gin.TestMode)

	body, _ := json.Marshal(map[string]any{"connection_ids": []string{"oa-1"}, "proxy_pool_id": "pool-a"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/providers/openai/connections/bulk-proxy", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = []gin.Param{{Key: "id", Value: "openai"}}
	h.BulkAssignProxy(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "proxy pool") {
		t.Errorf("expected proxy pool error, got %s", w.Body.String())
	}
}

// TestBulkAddConnections_NoContentionUnderLoad proves the fix: while a
// background goroutine hammers the SAME WriteQueue with writes, a 1000-row
// bulk import routed through the queue completes with zero failures (the
// single-writer queue serializes them, so no "database is locked").
func TestBulkAddConnections_NoContentionUnderLoad(t *testing.T) {
	logging.Init("text") // queue logs via logging.Logger, which is nil until Init
	database := newConnectionHandlerTestDB(t)
	seedProviderType(t, database, "bulktest")
	h := bulkHandlerWithQueue(t, database)

	gin.SetMode(gin.TestMode)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				// Hammer the SAME single-writer queue with blocking writes; the
				// queue serializes them with the bulk import, so no lock errors.
				_ = h.writeQueue.Do(context.Background(), "contention:write", func(d *sql.DB) error {
					_, err := d.Exec("UPDATE provider_types SET display_name = ? WHERE id = 'bulktest'", "x"+strconv.Itoa(i))
					i++
					return err
				})
			}
		}
	}()

	const n = 1000
	conns := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		conns = append(conns, map[string]any{
			"name":    "conn-" + strconv.Itoa(i),
			"api_key": "sk-" + strconv.Itoa(i),
			"priority": 1,
		})
	}
	body, _ := json.Marshal(map[string]any{"connections": conns})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/providers/bulktest/connections/bulk", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = []gin.Param{{Key: "id", Value: "bulktest"}}
	h.BulkAddConnections(c)

	close(stop)
	wg.Wait()

	var resp struct {
		Created int      `json:"created"`
		Failed  int      `json:"failed"`
		Errors  []string `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Failed != 0 {
		t.Fatalf("failed=%d under load (errors=%v)", resp.Failed, resp.Errors)
	}
}
