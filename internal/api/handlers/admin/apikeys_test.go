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

func TestAPIKeyHandler_List_IncludesAllowedModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newAPIKeyHandlerTestDB(t)
	h := NewAPIKeyHandler(database, middleware.NewAuthCache(30*time.Second))

	allowedJSON, _ := json.Marshal([]string{"gpt-4o", "claude-3.5-sonnet"})
	_, err := database.Exec(`INSERT INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at, allowed_models) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"key-models", "hash", "raw", "models-key", 60, 1000, 1, 1000, string(allowedJSON))
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
		Data []struct {
			ID            string   `json:"id"`
			AllowedModels []string `json:"allowed_models"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(listResp.Data) != 1 {
		t.Fatalf("expected 1 key, got %d", len(listResp.Data))
	}
	got := listResp.Data[0].AllowedModels
	want := []string{"gpt-4o", "claude-3.5-sonnet"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("list allowed_models = %v, want %v", got, want)
	}
}

func TestAPIKeyHandler_List_EmptyAllowedModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newAPIKeyHandlerTestDB(t)
	h := NewAPIKeyHandler(database, middleware.NewAuthCache(30*time.Second))

	_, err := database.Exec(`INSERT INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at, allowed_models) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"key-empty", "hash", "raw", "empty-key", 60, 1000, 1, 1000, nil)
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
		Data []struct {
			ID            string   `json:"id"`
			AllowedModels []string `json:"allowed_models"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(listResp.Data) != 1 {
		t.Fatalf("expected 1 key, got %d", len(listResp.Data))
	}
	if listResp.Data[0].AllowedModels != nil && len(listResp.Data[0].AllowedModels) != 0 {
		t.Errorf("list allowed_models = %v, want nil or empty", listResp.Data[0].AllowedModels)
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
func TestAPIKeyHandler_Create_IncludesAllowedModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newAPIKeyHandlerTestDB(t)
	h := NewAPIKeyHandler(database, middleware.NewAuthCache(30*time.Second))

	allowed := []string{"gpt-4o", "claude-3.5-sonnet"}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", jsonBodyAPIKey(t, map[string]any{
		"name":           "test-key-models",
		"max_tokens":     1000,
		"allowed_models": allowed,
	}))
	h.Create(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp struct {
		ID            string   `json:"id"`
		AllowedModels []string `json:"allowed_models"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.AllowedModels) != 2 || resp.AllowedModels[0] != "gpt-4o" || resp.AllowedModels[1] != "claude-3.5-sonnet" {
		t.Errorf("response allowed_models = %v, want %v", resp.AllowedModels, allowed)
	}

	var raw string
	if err := database.QueryRow(`SELECT COALESCE(allowed_models, '') FROM api_keys WHERE id = ?`, resp.ID).Scan(&raw); err != nil {
		t.Fatalf("query stored allowed_models: %v", err)
	}
	var stored []string
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		t.Fatalf("stored allowed_models is not valid JSON: %q, err=%v", raw, err)
	}
	if len(stored) != 2 || stored[0] != "gpt-4o" || stored[1] != "claude-3.5-sonnet" {
		t.Errorf("stored allowed_models = %v, want %v", stored, allowed)
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

func TestAPIKeyHandler_Create_IncludesBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newAPIKeyHandlerTestDB(t)
	h := NewAPIKeyHandler(database, middleware.NewAuthCache(30*time.Second))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", jsonBodyAPIKey(t, map[string]any{
		"name":              "budget-key",
		"daily_limit_usd":   5.0,
		"monthly_limit_usd": 50.0,
		"warning_threshold": 0.9,
	}))
	h.Create(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp struct {
		ID               string  `json:"id"`
		DailyLimitUSD    float64 `json:"daily_limit_usd"`
		MonthlyLimitUSD  float64 `json:"monthly_limit_usd"`
		WarningThreshold float64 `json:"warning_threshold"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.DailyLimitUSD != 5.0 {
		t.Errorf("daily_limit_usd = %v, want 5.0", resp.DailyLimitUSD)
	}
	if resp.MonthlyLimitUSD != 50.0 {
		t.Errorf("monthly_limit_usd = %v, want 50.0", resp.MonthlyLimitUSD)
	}
	if resp.WarningThreshold != 0.9 {
		t.Errorf("warning_threshold = %v, want 0.9", resp.WarningThreshold)
	}

	var stored struct {
		DailyLimitUSD    float64
		MonthlyLimitUSD  float64
		WarningThreshold float64
	}
	if err := database.QueryRow(`SELECT daily_limit_usd, monthly_limit_usd, warning_threshold FROM api_key_budgets WHERE api_key_id = ?`, resp.ID).Scan(
		&stored.DailyLimitUSD, &stored.MonthlyLimitUSD, &stored.WarningThreshold); err != nil {
		t.Fatalf("query stored budget: %v", err)
	}
	if stored.DailyLimitUSD != 5.0 || stored.MonthlyLimitUSD != 50.0 || stored.WarningThreshold != 0.9 {
		t.Errorf("stored budget = %+v, want 5/50/0.9", stored)
	}
}

func TestAPIKeyHandler_List_IncludesBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newAPIKeyHandlerTestDB(t)
	h := NewAPIKeyHandler(database, middleware.NewAuthCache(30*time.Second))

	_, err := database.Exec(`INSERT INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"key-budget-list", "hash", "raw", "budget-list", 60, 1000, 1, 1000)
	if err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	_, err = database.Exec(`INSERT INTO api_key_budgets (api_key_id, daily_limit_usd, monthly_limit_usd, warning_threshold, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"key-budget-list", 2.5, 25.0, 0.75, time.Now().Unix())
	if err != nil {
		t.Fatalf("seed budget: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/api-keys", nil)
	h.List(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var listResp struct {
		Data []struct {
			ID               string  `json:"id"`
			DailyLimitUSD    float64 `json:"daily_limit_usd"`
			MonthlyLimitUSD  float64 `json:"monthly_limit_usd"`
			WarningThreshold float64 `json:"warning_threshold"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(listResp.Data) != 1 {
		t.Fatalf("expected 1 key, got %d", len(listResp.Data))
	}
	item := listResp.Data[0]
	if item.DailyLimitUSD != 2.5 {
		t.Errorf("daily_limit_usd = %v, want 2.5", item.DailyLimitUSD)
	}
	if item.MonthlyLimitUSD != 25.0 {
		t.Errorf("monthly_limit_usd = %v, want 25.0", item.MonthlyLimitUSD)
	}
	if item.WarningThreshold != 0.75 {
		t.Errorf("warning_threshold = %v, want 0.75", item.WarningThreshold)
	}
}

func TestAPIKeyHandler_Get_IncludesBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newAPIKeyHandlerTestDB(t)
	h := NewAPIKeyHandler(database, middleware.NewAuthCache(30*time.Second))

	_, err := database.Exec(`INSERT INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"key-budget-get", "hash", "raw", "budget-get", 60, 1000, 1, 1000)
	if err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	_, err = database.Exec(`INSERT INTO api_key_budgets (api_key_id, daily_limit_usd, monthly_limit_usd, warning_threshold, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"key-budget-get", 3.0, 30.0, 0.85, time.Now().Unix())
	if err != nil {
		t.Fatalf("seed budget: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/api-keys/key-budget-get", nil)
	c.Params = gin.Params{{Key: "id", Value: "key-budget-get"}}
	h.Get(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			ID               string  `json:"id"`
			DailyLimitUSD    float64 `json:"daily_limit_usd"`
			MonthlyLimitUSD  float64 `json:"monthly_limit_usd"`
			WarningThreshold float64 `json:"warning_threshold"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Data.DailyLimitUSD != 3.0 {
		t.Errorf("daily_limit_usd = %v, want 3.0", resp.Data.DailyLimitUSD)
	}
	if resp.Data.MonthlyLimitUSD != 30.0 {
		t.Errorf("monthly_limit_usd = %v, want 30.0", resp.Data.MonthlyLimitUSD)
	}
	if resp.Data.WarningThreshold != 0.85 {
		t.Errorf("warning_threshold = %v, want 0.85", resp.Data.WarningThreshold)
	}
}

func TestAPIKeyHandler_ToggleActive_UpdatesBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database := newAPIKeyHandlerTestDB(t)
	h := NewAPIKeyHandler(database, middleware.NewAuthCache(30*time.Second))

	_, err := database.Exec(`INSERT INTO api_keys (id, key_hash, key_value, name, rate_limit_per_min, max_tokens, is_active, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"key-budget-toggle", "hash", "raw", "budget-toggle", 60, 1000, 1, 1000)
	if err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPatch, "/api/admin/api-keys/key-budget-toggle/toggle", jsonBodyAPIKey(t, map[string]any{
		"is_active":         false,
		"daily_limit_usd":   4.0,
		"monthly_limit_usd": 40.0,
		"warning_threshold": 0.95,
	}))
	c.Params = gin.Params{{Key: "id", Value: "key-budget-toggle"}}
	h.ToggleActive(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var stored struct {
		DailyLimitUSD    float64
		MonthlyLimitUSD  float64
		WarningThreshold float64
	}
	if err := database.QueryRow(`SELECT daily_limit_usd, monthly_limit_usd, warning_threshold FROM api_key_budgets WHERE api_key_id = ?`, "key-budget-toggle").Scan(
		&stored.DailyLimitUSD, &stored.MonthlyLimitUSD, &stored.WarningThreshold); err != nil {
		t.Fatalf("query stored budget: %v", err)
	}
	if stored.DailyLimitUSD != 4.0 || stored.MonthlyLimitUSD != 40.0 || stored.WarningThreshold != 0.95 {
		t.Errorf("stored budget = %+v, want 4/40/0.95", stored)
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
