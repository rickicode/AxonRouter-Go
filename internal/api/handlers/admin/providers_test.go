package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
	"github.com/rickicode/AxonRouter-Go/internal/executor"
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
	return NewProviderHandler(database, executor.GetRegistry(), store, elig, providerCfg)
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
	h := NewProviderHandler(database, registry, store, elig, providerCfg)

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
