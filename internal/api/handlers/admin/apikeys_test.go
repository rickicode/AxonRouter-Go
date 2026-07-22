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
	_ "modernc.org/sqlite"

	"github.com/rickicode/AxonRouter-Go/internal/api/middleware"
	"github.com/rickicode/AxonRouter-Go/internal/db"
)

func newAPIKeyHandlerTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "apikey-handler-test.db")
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

func jsonBodyAPIKey(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bytes.NewBuffer(b)
}

func TestAPIKeyHandler_Create_IncludesExpiresAt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newAPIKeyHandlerTestDB(t)
	h := NewAPIKeyHandler(database, middleware.NewAuthCache(30*time.Second))

	exp := int64(1893456000)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", jsonBodyAPIKey(t, map[string]any{
		"name":       "test-key",
		"max_tokens": 1000,
		"expires_at": exp,
	}))
	h.Create(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["expires_at"] != float64(exp) {
		t.Errorf("response expires_at = %v, want %v", resp["expires_at"], exp)
	}

	var stored int64
	if err := database.QueryRow(`SELECT COALESCE(expires_at, 0) FROM api_keys WHERE id = ?`, resp["id"]).Scan(&stored); err != nil {
		t.Fatalf("query stored expires_at: %v", err)
	}
	if stored != exp {
		t.Errorf("stored expires_at = %d, want %d", stored, exp)
	}
}

func TestAPIKeyHandler_Create_NoExpiresAt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newAPIKeyHandlerTestDB(t)
	h := NewAPIKeyHandler(database, middleware.NewAuthCache(30*time.Second))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", jsonBodyAPIKey(t, map[string]any{
		"name":       "test-key-no-exp",
		"max_tokens": 1000,
	}))
	h.Create(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["expires_at"] != float64(0) {
		t.Errorf("response expires_at = %v, want 0", resp["expires_at"])
	}

	var id string
	if err := database.QueryRow(`SELECT id FROM api_keys WHERE name = ?`, "test-key-no-exp").Scan(&id); err != nil {
		t.Fatalf("query key: %v", err)
	}
	var stored sql.NullInt64
	if err := database.QueryRow(`SELECT expires_at FROM api_keys WHERE id = ?`, id).Scan(&stored); err != nil {
		t.Fatalf("query stored expires_at: %v", err)
	}
	if stored.Valid != false {
		t.Errorf("stored expires_at should be NULL, got valid=%v value=%d", stored.Valid, stored.Int64)
	}
}

func TestAPIKeyHandler_List_IncludesExpiresAt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newAPIKeyHandlerTestDB(t)
	h := NewAPIKeyHandler(database, middleware.NewAuthCache(30*time.Second))

	exp := int64(1893456000)
	_, err := database.Exec(`INSERT INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"key-1", "hash", "raw", "listed", 60, 1000, 1, 1000, exp)
	if err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/api-keys", nil)
	h.List(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var listResp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(listResp.Data) != 1 {
		t.Fatalf("expected 1 key, got %d", len(listResp.Data))
	}
	if listResp.Data[0]["expires_at"] != float64(exp) {
		t.Errorf("list expires_at = %v, want %v", listResp.Data[0]["expires_at"], exp)
	}
}
func TestAPIKeyHandler_Create_PastExpiresAt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newAPIKeyHandlerTestDB(t)
	h := NewAPIKeyHandler(database, middleware.NewAuthCache(30*time.Second))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", jsonBodyAPIKey(t, map[string]any{
		"name":       "past-key",
		"expires_at": time.Now().Unix() - 10,
	}))
	h.Create(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAPIKeyHandler_ToggleActive_KeepsExpiresAt_WhenOmitted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newAPIKeyHandlerTestDB(t)
	h := NewAPIKeyHandler(database, middleware.NewAuthCache(30*time.Second))

	existingExp := int64(1900000000)
	_, err := database.Exec(`INSERT INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"key-keep", "hash", "raw", "keep", 60, 1000, 1, 1000, existingExp)
	if err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/api-keys/key-keep/toggle", jsonBodyAPIKey(t, map[string]any{
		"is_active":  false,
		"max_tokens": 2000,
	}))
	c.Params = gin.Params{{Key: "id", Value: "key-keep"}}
	h.ToggleActive(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var stored int64
	if err := database.QueryRow(`SELECT COALESCE(expires_at, 0) FROM api_keys WHERE id = ?`, "key-keep").Scan(&stored); err != nil {
		t.Fatalf("query expires_at: %v", err)
	}
	if stored != existingExp {
		t.Errorf("expires_at = %d, want %d (should be unchanged)", stored, existingExp)
	}
}

func TestAPIKeyHandler_ToggleInvalidatesCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newAPIKeyHandlerTestDB(t)
	cache := middleware.NewAuthCache(30 * time.Second)
	h := NewAPIKeyHandler(database, cache)

	// Seed an active key. key_value is what clients present and what the cache keys by.
	_, err := database.Exec(`INSERT INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"key-toggle", "hash", "raw-toggle", "toggle-key", 60, 1000, 1, 1000, 0)
	if err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	// Populate the cache entry as if a recent request validated the key.
	cache.Put("raw-toggle", "key-toggle", 60, 1000, 0, nil)
	if r := cache.Get("raw-toggle"); r == nil {
		t.Fatal("expected cached entry before toggle")
	}

	// Toggle the key inactive.
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/api-keys/key-toggle/toggle", jsonBodyAPIKey(t, map[string]any{
		"is_active": false,
	}))
	c.Params = gin.Params{{Key: "id", Value: "key-toggle"}}
	h.ToggleActive(c)

	if w.Code != http.StatusOK {
		t.Fatalf("toggle status = %d, body = %s", w.Code, w.Body.String())
	}

	// The cache entry must be invalidated immediately, so a request within the
	// 30s TTL would now hit the DB and see is_active=0.
	if r := cache.Get("raw-toggle"); r != nil {
		t.Errorf("expected cache entry to be invalidated after toggle, got %+v", r)
	}
}

func TestAPIKeyHandler_ToggleActive_MaxTokensZero_SetsUnlimited(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newAPIKeyHandlerTestDB(t)
	h := NewAPIKeyHandler(database, middleware.NewAuthCache(30*time.Second))

	_, err := database.Exec(`INSERT INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"key-unlim", "hash", "raw-unlim", "unlimited-key", 60, 1000, 1, 1000, 0)
	if err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/api-keys/key-unlim/toggle", jsonBodyAPIKey(t, map[string]any{
		"is_active":  false,
		"max_tokens": 0,
	}))
	c.Params = gin.Params{{Key: "id", Value: "key-unlim"}}
	h.ToggleActive(c)

	if w.Code != http.StatusOK {
		t.Fatalf("toggle status = %d, body = %s", w.Code, w.Body.String())
	}

	var storedMax sql.NullInt64
	if err := database.QueryRow(`SELECT max_tokens FROM api_keys WHERE id = ?`, "key-unlim").Scan(&storedMax); err != nil {
		t.Fatalf("query max_tokens: %v", err)
	}
	if storedMax.Valid && storedMax.Int64 != 0 {
		t.Errorf("max_tokens = %d, want 0 (unlimited)", storedMax.Int64)
	}
}

func TestAPIKeyHandler_Delete_WithUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newAPIKeyHandlerTestDB(t)
	h := NewAPIKeyHandler(database, middleware.NewAuthCache(30*time.Second))

	_, err := database.Exec(`INSERT INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"key-usage", "hash", "raw-usage", "usage-key", 60, 1000, 1, 1000, 0)
	if err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	_, err = database.Exec(`INSERT INTO api_key_usage (api_key_id, total_tokens, updated_at) VALUES (?, ?, ?)`,
		"key-usage", 500, 1000)
	if err != nil {
		t.Fatalf("seed api key usage: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/admin/api-keys/key-usage", nil)
	c.Params = gin.Params{{Key: "id", Value: "key-usage"}}
	h.Delete(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM api_keys WHERE id = ?`, "key-usage").Scan(&count); err != nil {
		t.Fatalf("query api_keys: %v", err)
	}
	if count != 0 {
		t.Errorf("api_keys count = %d, want 0", count)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM api_key_usage WHERE api_key_id = ?`, "key-usage").Scan(&count); err != nil {
		t.Fatalf("query api_key_usage: %v", err)
	}
	if count != 0 {
		t.Errorf("api_key_usage count = %d, want 0", count)
	}
}
