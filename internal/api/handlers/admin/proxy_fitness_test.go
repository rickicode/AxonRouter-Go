package admin

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/proxypool"
)

// seedFitnessPool inserts a minimal proxy pool row directly (no health check).
func seedFitnessPool(t *testing.T, database *sql.DB, id, name string) {
	t.Helper()
	now := time.Now().Unix()
	_, err := database.Exec(`INSERT INTO proxy_pools (id, name, type, proxy_url, is_active, test_status, created_at, updated_at) VALUES (?, ?, 'http', 'http://pool.example:8080', 1, 'active', ?, ?)`, id, name, now, now)
	if err != nil {
		t.Fatalf("seed pool: %v", err)
	}
}

// TestProxyFitnessList returns marks, geo, and names.
func TestProxyFitnessList(t *testing.T) {
	database := newProxyPoolTestDB(t)
	gin.SetMode(gin.TestMode)
	h := NewProxyPoolHandler(database, nil, proxypool.NewResolver(database), nil)

	seedFitnessPool(t, database, "pool-1", "Alpha")
	seedFitnessPool(t, database, "pool-2", "Beta")

	// Clean singleton state between runs.
	proxypool.Fitness().ClearAll()
	defer proxypool.Fitness().ClearAll()
	proxypool.Geo().ClearAll()
	defer proxypool.Geo().ClearAll()

	proxypool.Fitness().MarkUnfit("pool-1", "freebuff::gpt-4o", "limited_ip", 5*time.Minute)
	proxypool.Geo().Record("pool-2", "1.2.3.4", "US", "CA", "San Jose", "Example LLC", time.Now())

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/proxy-pools/fitness", nil)
	h.FitnessList(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Success bool                              `json:"success"`
		Pools   map[string]map[string]proxypool.FitnessMark `json:"pools"`
		Geo     map[string]proxypool.GeoEntry     `json:"geo"`
		Names   map[string]string                 `json:"names"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Success {
		t.Fatal("success = false")
	}
	if len(resp.Names) != 2 {
		t.Fatalf("names len = %d, want 2", len(resp.Names))
	}
	if resp.Names["pool-1"] != "Alpha" || resp.Names["pool-2"] != "Beta" {
		t.Fatalf("names = %+v", resp.Names)
	}
	mark, ok := resp.Pools["pool-1"]["freebuff::gpt-4o"]
	if !ok {
		t.Fatalf("missing fitness mark for pool-1/freebuff::gpt-4o, pools=%+v", resp.Pools)
	}
	if mark.Reason != "limited_ip" {
		t.Fatalf("reason = %q, want limited_ip", mark.Reason)
	}
	geo, ok := resp.Geo["pool-2"]
	if !ok {
		t.Fatalf("missing geo entry for pool-2, geo=%+v", resp.Geo)
	}
	if geo.IP != "1.2.3.4" || geo.Country != "US" || geo.Org != "Example LLC" {
		t.Fatalf("geo = %+v", geo)
	}
}

// TestProxyFitnessClear removes a single scope, then all scopes.
func TestProxyFitnessClear(t *testing.T) {
	database := newProxyPoolTestDB(t)
	gin.SetMode(gin.TestMode)
	h := NewProxyPoolHandler(database, nil, proxypool.NewResolver(database), nil)

	seedFitnessPool(t, database, "pool-1", "Alpha")
	proxypool.Fitness().ClearAll()
	defer proxypool.Fitness().ClearAll()

	proxypool.Fitness().MarkUnfit("pool-1", "freebuff::gpt-4o", "limited_ip", 5*time.Minute)
	proxypool.Fitness().MarkUnfit("pool-1", "freebuff::gpt-5", "limited_ip", 5*time.Minute)

	// Clear one scope.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "pool-1"}}
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy-pools/pool-1/fitness/clear", jsonBodyProxyPool(t, map[string]any{"scope": "freebuff::gpt-4o"}))
	h.FitnessClear(c)
	if w.Code != http.StatusOK {
		t.Fatalf("clear status = %d, body=%s", w.Code, w.Body.String())
	}
	if !proxypool.Fitness().IsFit("pool-1", "freebuff::gpt-4o") {
		t.Fatal("pool-1 should be fit again for freebuff::gpt-4o after scope clear")
	}
	if proxypool.Fitness().IsFit("pool-1", "freebuff::gpt-5") {
		t.Fatal("pool-1 should stay unfit for freebuff::gpt-5")
	}

	// Clear all scopes for the pool (empty scope).
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "pool-1"}}
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy-pools/pool-1/fitness/clear", jsonBodyProxyPool(t, map[string]any{}))
	h.FitnessClear(c)
	if w.Code != http.StatusOK {
		t.Fatalf("clear-all-scopes status = %d, body=%s", w.Code, w.Body.String())
	}
	if !proxypool.Fitness().IsFit("pool-1", "freebuff::gpt-5") {
		t.Fatal("pool-1 should be fit for freebuff::gpt-5 after clearing all scopes")
	}
}

// TestProxyFitnessClearUnknownPool returns 404.
func TestProxyFitnessClearUnknownPool(t *testing.T) {
	database := newProxyPoolTestDB(t)
	gin.SetMode(gin.TestMode)
	h := NewProxyPoolHandler(database, nil, proxypool.NewResolver(database), nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy-pools/nope/fitness/clear", jsonBodyProxyPool(t, map[string]any{}))
	h.FitnessClear(c)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// TestProxyFitnessClearAll removes every mark, and honors the provider filter.
func TestProxyFitnessClearAll(t *testing.T) {
	database := newProxyPoolTestDB(t)
	gin.SetMode(gin.TestMode)
	h := NewProxyPoolHandler(database, nil, proxypool.NewResolver(database), nil)

	proxypool.Fitness().ClearAll()
	defer proxypool.Fitness().ClearAll()

	proxypool.Fitness().MarkUnfit("pool-1", "freebuff::gpt-4o", "limited_ip", 5*time.Minute)
	proxypool.Fitness().MarkUnfit("pool-1", "openai::gpt-4o", "limited_ip", 5*time.Minute)

	// Provider-filtered clear: only freebuff::* scopes removed.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy-pools/fitness/clear-all", jsonBodyProxyPool(t, map[string]any{"provider": "freebuff"}))
	h.FitnessClearAll(c)
	if w.Code != http.StatusOK {
		t.Fatalf("clear-all status = %d, body=%s", w.Code, w.Body.String())
	}
	if !proxypool.Fitness().IsFit("pool-1", "freebuff::gpt-4o") {
		t.Fatal("freebuff mark should be cleared by provider filter")
	}
	if proxypool.Fitness().IsFit("pool-1", "openai::gpt-4o") {
		t.Fatal("openai mark should survive provider filter")
	}

	// Unfiltered clear removes everything.
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy-pools/fitness/clear-all", jsonBodyProxyPool(t, map[string]any{}))
	h.FitnessClearAll(c)
	if w.Code != http.StatusOK {
		t.Fatalf("clear-all status = %d, body=%s", w.Code, w.Body.String())
	}
	snap := proxypool.Fitness().Snapshot()
	if len(snap) != 0 {
		t.Fatalf("snapshot len = %d, want 0 after clear-all", len(snap))
	}
}
