package admin

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

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

func TestProxyPoolBulkCreate(t *testing.T) {
	database := newProxyPoolTestDB(t)
	gin.SetMode(gin.TestMode)
	h := NewProxyPoolHandler(database, nil, proxypool.NewResolver(database))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy-pools/bulk", jsonBodyProxyPool(t, map[string]any{
		"items": []any{
			"http://proxy1.example:8080",
			"http://proxy2.example:8080",
			map[string]any{"name": "custom-pool", "proxyUrl": "http://proxy3.example:8080", "type": "http"},
		},
		"namePrefix":  "bulk",
		"defaultType": "http",
		"isActive":    true,
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
		"items":       []any{"http://proxy1.example:8080", "http://proxy4.example:8080"},
		"namePrefix":  "bulk",
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

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy-pools/bulk", jsonBodyProxyPool(t, map[string]any{
		"items":       []any{"us-proxy|http://proxy-us.example:8080", "http://proxy-eu.example:8080"},
		"namePrefix":  "bulk",
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

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/proxy-pools/bulk", jsonBodyProxyPool(t, map[string]any{
		"items":       []any{"https://myrelay.vercel.app"},
		"namePrefix":  "relay",
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
