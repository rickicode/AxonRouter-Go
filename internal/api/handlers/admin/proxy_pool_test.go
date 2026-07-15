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
	"github.com/rickicode/AxonRouter-Go/internal/proxypool"
	_ "modernc.org/sqlite"
)

func jsonBodyProxyPool(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bytes.NewBuffer(b)
}

func newProxyPoolTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "proxy-pool-test.db")
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

func noopTestProxy(_ string, _ string, _ string) proxypool.TestResult {
	return proxypool.TestResult{OK: true, StatusCode: 200, ElapsedMs: 0}
}

func TestProxyPoolBulkCreate(t *testing.T) {
	database := newProxyPoolTestDB(t)
	gin.SetMode(gin.TestMode)
	h := NewProxyPoolHandler(database, nil, proxypool.NewResolver(database))
	h.testProxy = noopTestProxy

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy-pools/bulk", jsonBodyProxyPool(t, map[string]any{
		"items": []any{
			"http://proxy1.example:8080",
			"http://proxy2.example:8080",
			map[string]any{"name": "custom-pool", "proxyUrl": "http://proxy3.example:8080", "type": "http"},
		},
		"namePrefix": "bulk",
		"defaultType": "http",
		"isActive": true,
	}))
	h.BulkCreate(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("BulkCreate status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Created int `json:"created"`
		Skipped int `json:"skipped"`
		Errors int `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Created != 3 {
		t.Fatalf("expected 3 created, got %+v", resp)
	}
	if resp.Skipped != 0 || resp.Errors != 0 {
		t.Fatalf("expected no skips/errors, got %+v", resp)
	}

	// Second batch with duplicate should skip.
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy-pools/bulk", jsonBodyProxyPool(t, map[string]any{
		"items": []any{"http://proxy1.example:8080", "http://proxy4.example:8080"},
		"namePrefix": "bulk",
		"defaultType": "http",
	}))
	h.BulkCreate(c)
	if w.Code != http.StatusCreated {
		t.Fatalf("second BulkCreate status = %d, body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Created != 1 || resp.Skipped != 1 {
		t.Fatalf("expected 1 created + 1 skipped, got %+v", resp)
	}
}

func TestProxyPoolBulkCreateNamePipe(t *testing.T) {
	database := newProxyPoolTestDB(t)
	gin.SetMode(gin.TestMode)
	h := NewProxyPoolHandler(database, nil, proxypool.NewResolver(database))
	h.testProxy = noopTestProxy

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy-pools/bulk", jsonBodyProxyPool(t, map[string]any{
		"items": []any{"us-proxy|http://proxy-us.example:8080", "http://proxy-eu.example:8080"},
		"namePrefix": "bulk",
		"defaultType": "http",
	}))
	h.BulkCreate(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("BulkCreate status = %d, body=%s", w.Code, w.Body.String())
	}

	row := database.QueryRow("SELECT name, proxy_url FROM proxy_pools WHERE proxy_url = ?", "http://proxy-us.example:8080")
	var gotName, gotURL string
	if err := row.Scan(&gotName, &gotURL); err != nil {
		t.Fatalf("find pool: %v", err)
	}
	if gotName != "us-proxy" {
		t.Fatalf("expected name us-proxy, got %s", gotName)
	}
}

func TestProxyPoolBulkCreateDetectsRelayType(t *testing.T) {
	database := newProxyPoolTestDB(t)
	gin.SetMode(gin.TestMode)
	h := NewProxyPoolHandler(database, nil, proxypool.NewResolver(database))
	h.testProxy = noopTestProxy

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy-pools/bulk", jsonBodyProxyPool(t, map[string]any{
		"items": []any{"https://myrelay.vercel.app"},
		"namePrefix": "relay",
		"defaultType": "http",
	}))
	h.BulkCreate(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("BulkCreate status = %d, body=%s", w.Code, w.Body.String())
	}
	var typ, relayAuth string
	err := database.QueryRow("SELECT type, relay_auth FROM proxy_pools WHERE proxy_url = ?", "https://myrelay.vercel.app").Scan(&typ, &relayAuth)
	if err != nil {
		t.Fatalf("find relay pool: %v", err)
	}
	if typ != "vercel" {
		t.Fatalf("expected relay type vercel, got %s", typ)
	}
	if relayAuth == "" {
		t.Fatal("expected relay_auth to be generated")
	}
}

func TestProxyPoolBulkCreateUsesTestHook(t *testing.T) {
	database := newProxyPoolTestDB(t)
	gin.SetMode(gin.TestMode)
	h := NewProxyPoolHandler(database, nil, proxypool.NewResolver(database))
	called := false
	h.testProxy = func(proxyURL, typ, auth string) proxypool.TestResult {
		called = true
		if proxyURL == "http://stub.example:8080" {
			return proxypool.TestResult{OK: true, StatusCode: 200, IP: "1.2.3.4", Country: "US", City: "NYC", Org: "TestOrg", ElapsedMs: 12}
		}
		return proxypool.TestResult{OK: false, Error: "unexpected URL"}
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy-pools/bulk", jsonBodyProxyPool(t, map[string]any{
		"items":          []any{"http://stub.example:8080"},
		"namePrefix":     "bulk",
		"defaultType":    "http",
		"isActive":       true,
		"requireHealthy": true,
	}))
	h.BulkCreate(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("BulkCreate status = %d, body=%s", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("expected testProxy hook to be called")
	}

	var status, ip, country, city, org string
	err := database.QueryRow("SELECT test_status, proxy_ip, proxy_country, proxy_city, proxy_org FROM proxy_pools WHERE proxy_url = ?", "http://stub.example:8080").Scan(&status, &ip, &country, &city, &org)
	if err != nil {
		t.Fatalf("find created pool: %v", err)
	}
	if status != "active" {
		t.Fatalf("expected test_status active, got %s", status)
	}
	if ip != "1.2.3.4" || country != "US" || city != "NYC" || org != "TestOrg" {
		t.Fatalf("expected proxy metadata from hook, got ip=%s country=%s city=%s org=%s", ip, country, city, org)
	}
}

func TestProxyPoolBulkCreateSkipsUnhealthy(t *testing.T) {
	database := newProxyPoolTestDB(t)
	gin.SetMode(gin.TestMode)
	h := NewProxyPoolHandler(database, nil, proxypool.NewResolver(database))
	h.testProxy = func(proxyURL, typ, auth string) proxypool.TestResult {
		if proxyURL == "http://good.example:8080" {
			return proxypool.TestResult{OK: true, StatusCode: 200, IP: "1.2.3.4", ElapsedMs: 100}
		}
		return proxypool.TestResult{OK: false, Error: "timeout"}
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy-pools/bulk", jsonBodyProxyPool(t, map[string]any{
		"items": []any{
			"http://good.example:8080",
			"http://bad.example:8080",
		},
		"defaultType":    "http",
		"requireHealthy": true,
	}))
	h.BulkCreate(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("BulkCreate status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Created int `json:"created"`
		Skipped int `json:"skipped"`
		Errors  int `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Created != 1 || resp.Skipped != 1 || resp.Errors != 0 {
		t.Fatalf("expected 1 created + 1 skipped, got %+v", resp)
	}

	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM proxy_pools WHERE proxy_url = ?", "http://bad.example:8080").Scan(&count); err != nil {
		t.Fatalf("count bad pool: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected unhealthy pool to not be inserted, got %d", count)
	}
}

func TestProxyPoolBulkCreateSkipsSlow(t *testing.T) {
	database := newProxyPoolTestDB(t)
	gin.SetMode(gin.TestMode)
	h := NewProxyPoolHandler(database, nil, proxypool.NewResolver(database))
	h.testProxy = func(proxyURL, typ, auth string) proxypool.TestResult {
		if proxyURL == "http://fast.example:8080" {
			return proxypool.TestResult{OK: true, StatusCode: 200, ElapsedMs: 500}
		}
		return proxypool.TestResult{OK: true, StatusCode: 200, ElapsedMs: 1500}
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy-pools/bulk", jsonBodyProxyPool(t, map[string]any{
		"items": []any{
			"http://fast.example:8080",
			"http://slow.example:8080",
		},
		"defaultType":       "http",
		"requireHealthy":    true,
		"maxResponseTimeMs": 1000,
	}))
	h.BulkCreate(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("BulkCreate status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Created int `json:"created"`
		Skipped int `json:"skipped"`
		Errors  int `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Created != 1 || resp.Skipped != 1 || resp.Errors != 0 {
		t.Fatalf("expected 1 created + 1 skipped, got %+v", resp)
	}

	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM proxy_pools WHERE proxy_url = ?", "http://slow.example:8080").Scan(&count); err != nil {
		t.Fatalf("count slow pool: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected slow pool to not be inserted, got %d", count)
	}
}

func TestProxyPoolBulkDelete(t *testing.T) {
	database := newProxyPoolTestDB(t)
	gin.SetMode(gin.TestMode)
	h := NewProxyPoolHandler(database, nil, proxypool.NewResolver(database))

	now := time.Now().Unix()
	mustExec := func(query string, args ...any) {
		t.Helper()
		if _, err := database.Exec(query, args...); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	insert := func(id, status string) {
		t.Helper()
		mustExec(
			`INSERT INTO proxy_pools (id, name, type, proxy_url, no_proxy, relay_auth, is_active, test_status, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, id, "http", "http://"+id+".example:8080", "", "", 1, status, now, now,
		)
	}

	insert("pool-a", "error")
	insert("pool-b", "error")
	insert("pool-c", "ok")
	insert("pool-d", "ok")

	// Delete by test_status filter.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy-pools/bulk-delete", jsonBodyProxyPool(t, map[string]any{"status": "error"}))
	h.BulkDelete(c)
	if w.Code != http.StatusOK {
		t.Fatalf("BulkDelete status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp1 struct {
		Ok      bool `json:"ok"`
		Deleted int  `json:"deleted"`
		Skipped int  `json:"skipped"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp1.Ok || resp1.Deleted != 2 || resp1.Skipped != 0 {
		t.Fatalf("expected ok=true deleted=2 skipped=0, got %+v", resp1)
	}

	// Delete by explicit IDs, mixing an already-deleted and a remaining pool.
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy-pools/bulk-delete", jsonBodyProxyPool(t, map[string]any{"ids": []string{"pool-a", "pool-c"}}))
	h.BulkDelete(c)
	if w.Code != http.StatusOK {
		t.Fatalf("BulkDelete status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp2 struct {
		Ok      bool `json:"ok"`
		Deleted int  `json:"deleted"`
		Skipped int  `json:"skipped"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp2.Ok || resp2.Deleted != 1 || resp2.Skipped != 1 {
		t.Fatalf("expected ok=true deleted=1 skipped=1, got %+v", resp2)
	}

	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM proxy_pools").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 remaining pool, got %d", count)
	}
}

func TestProxyPoolDeleteCascadesDirectConnections(t *testing.T) {
	database := newProxyPoolTestDB(t)
	gin.SetMode(gin.TestMode)
	h := NewProxyPoolHandler(database, nil, proxypool.NewResolver(database))
	now := time.Now().Unix()
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := database.Exec(q, args...); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	mustExec(`INSERT INTO proxy_pools (id,name,type,proxy_url,is_active,test_status,created_at,updated_at) VALUES ('pool-x','x','http','http://x:1',1,'ok',?,?)`, now, now)
	mustExec(`INSERT INTO connections (id,provider_type_id,name,auth_type,status,is_active,provider_specific_data,created_at,updated_at) VALUES ('conn-pool','oc','c-pool','none','ready',1,'{"proxyPoolId":"pool-x"}',?,?)`, now, now)
	mustExec(`INSERT INTO connections (id,provider_type_id,name,auth_type,status,is_active,provider_specific_data,created_at,updated_at) VALUES ('conn-other','oc','c-other','none','ready',1,'{"proxyPoolId":"pool-y"}',?,?)`, now, now)

	if err := h.deletePoolCascade("pool-x"); err != nil {
		t.Fatalf("deletePoolCascade: %v", err)
	}

	var active1, active2 int
	if err := database.QueryRow("SELECT is_active FROM connections WHERE id='conn-pool'").Scan(&active1); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if err := database.QueryRow("SELECT is_active FROM connections WHERE id='conn-other'").Scan(&active2); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if active1 != 0 {
		t.Errorf("conn-pool should be soft-deleted (is_active=0), got %d", active1)
	}
	if active2 != 1 {
		t.Errorf("conn-other should survive, got %d", active2)
	}
	var pc int
	if err := database.QueryRow("SELECT COUNT(*) FROM proxy_pools WHERE id='pool-x'").Scan(&pc); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if pc != 0 {
		t.Errorf("pool-x should be deleted")
	}
}

func TestProxyPoolDeleteCascadesViaGroup(t *testing.T) {
	database := newProxyPoolTestDB(t)
	gin.SetMode(gin.TestMode)
	h := NewProxyPoolHandler(database, nil, proxypool.NewResolver(database))
	now := time.Now().Unix()
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := database.Exec(q, args...); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	mustExec(`INSERT INTO proxy_pools (id,name,type,proxy_url,is_active,test_status,created_at,updated_at) VALUES ('pool-g','g','http','http://g:1',1,'ok',?,?)`, now, now)
	mustExec(`INSERT INTO proxy_groups (id,name,mode,sticky_limit,strict_proxy,proxy_pool_ids,is_active,created_at,updated_at) VALUES ('grp-1','grp','round_robin',0,0,'["pool-g"]',1,?,?)`, now, now)
	mustExec(`INSERT INTO connections (id,provider_type_id,name,auth_type,status,is_active,provider_specific_data,created_at,updated_at) VALUES ('conn-g','oc','c-g','none','ready',1,'{"proxyGroupId":"grp-1"}',?,?)`, now, now)

	if err := h.deletePoolCascade("pool-g"); err != nil {
		t.Fatalf("deletePoolCascade: %v", err)
	}

	var active int
	if err := database.QueryRow("SELECT is_active FROM connections WHERE id='conn-g'").Scan(&active); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if active != 0 {
		t.Errorf("conn-g should be soft-deleted via group, got %d", active)
	}
	var gc int
	if err := database.QueryRow("SELECT COUNT(*) FROM proxy_groups WHERE id='grp-1'").Scan(&gc); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if gc != 0 {
		t.Errorf("empty group grp-1 should be deleted, got %d", gc)
	}
}

func TestProxyPoolDeleteKeepsDefaultDirectConnection(t *testing.T) {
	database := newProxyPoolTestDB(t)
	gin.SetMode(gin.TestMode)
	h := NewProxyPoolHandler(database, nil, proxypool.NewResolver(database))
	now := time.Now().Unix()
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := database.Exec(q, args...); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	mustExec(`INSERT INTO proxy_pools (id,name,type,proxy_url,is_active,test_status,created_at,updated_at) VALUES ('pool-d','d','http','http://d:1',1,'ok',?,?)`, now, now)
	// Default direct oc connection references no pool; must survive.
	mustExec(`INSERT INTO connections (id,provider_type_id,name,auth_type,status,is_active,provider_specific_data,created_at,updated_at) VALUES ('conn-direct','oc','c-direct','none','ready',1,'{"direct":"true"}',?,?)`, now, now)

	if err := h.deletePoolCascade("pool-d"); err != nil {
		t.Fatalf("deletePoolCascade: %v", err)
	}
	var active int
	if err := database.QueryRow("SELECT is_active FROM connections WHERE id='conn-direct'").Scan(&active); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if active != 1 {
		t.Errorf("default direct oc connection must survive, got %d", active)
	}
}
