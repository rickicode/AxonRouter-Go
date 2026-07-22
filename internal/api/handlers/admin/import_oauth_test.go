package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
)

// stubOAuthService satisfies auth.OAuthService without making network calls.
type stubOAuthService struct{}

func (stubOAuthService) GenerateAuthURL(context.Context, string) (string, error) {
	return "", nil
}

func (stubOAuthService) ExchangeCode(context.Context, string) (*auth.Credentials, error) {
	return nil, nil
}

func (stubOAuthService) RefreshToken(context.Context, *auth.Credentials) (*auth.Credentials, error) {
	return nil, nil
}

func (stubOAuthService) StartLocalServer(context.Context, string) (int, chan *auth.Credentials, error) {
	return 0, nil, nil
}

func newOAuthImportTestDeps(t *testing.T) (*OAuthHandler, *sql.DB) {
	t.Helper()
	database := newConnectionHandlerTestDB(t)
	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	authMgr := auth.NewManager()
	authMgr.RegisterService(auth.ProviderGrokCli, stubOAuthService{})
	return NewOAuthHandler(database, authMgr, store, elig), database
}

func TestImportToken_Success(t *testing.T) {
	h, database := newOAuthImportTestDeps(t)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{
		"provider": "grok-cli",
		"access_token": "access-123",
		"refresh_token": "refresh-456",
		"expires_at": 1893456000,
		"email": "user@example.com",
		"provider_specific_data": {"deviceId": "dev-abc"}
	}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/oauth/import-token", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ImportToken(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Status != "ready" {
		t.Fatalf("status = %q, want ready", resp.Status)
	}
	if resp.Name != "user@example.com" {
		t.Fatalf("name = %q, want user@example.com", resp.Name)
	}

	var authType, accessTok, refreshTok string
	var expiresAt int64
	var psdRaw string
	err := database.QueryRow(`
		SELECT auth_type, COALESCE(oauth_token,''), COALESCE(oauth_refresh_token,''), COALESCE(oauth_expires_at,0), COALESCE(provider_specific_data,'')
		FROM connections WHERE id = ?
	`, resp.ID).Scan(&authType, &accessTok, &refreshTok, &expiresAt, &psdRaw)
	if err != nil {
		t.Fatalf("fetch connection: %v", err)
	}
	if authType != "oauth" {
		t.Errorf("auth_type = %q, want oauth", authType)
	}
	if accessTok != "access-123" {
		t.Errorf("access_token = %q, want access-123", accessTok)
	}
	if refreshTok != "refresh-456" {
		t.Errorf("refresh_token = %q, want refresh-456", refreshTok)
	}
	if expiresAt != 1893456000 {
		t.Errorf("expires_at = %d, want 1893456000", expiresAt)
	}
	var psd map[string]string
	if err := json.Unmarshal([]byte(psdRaw), &psd); err != nil {
		t.Fatalf("psd unmarshal: %v", err)
	}
	if psd["deviceId"] != "dev-abc" {
		t.Errorf("deviceId = %q, want dev-abc", psd["deviceId"])
	}

	if cs := h.store.Get(resp.ID); cs == nil || cs.Status != connstate.StatusReady {
		t.Fatalf("in-memory status should be ready, got %v", cs)
	}
	if cs := h.store.Get(resp.ID); cs == nil || cs.Prefix != "grok-cli" {
		t.Fatalf("expected connection prefix grok-cli, got cs=%v", cs)
	}
	found := false
	for _, id := range h.elig.GetByPrefix("grok-cli") {
		if id == resp.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected connection %s in grok-cli eligible list", resp.ID)
	}
}

func TestImportToken_MissingAccessToken(t *testing.T) {
	h, _ := newOAuthImportTestDeps(t)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"provider":"grok-cli","refresh_token":"refresh-456","expires_at":1893456000}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/oauth/import-token", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ImportToken(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestImportToken_MissingRefreshToken(t *testing.T) {
	h, _ := newOAuthImportTestDeps(t)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"provider":"grok-cli","access_token":"access-123","expires_at":1893456000}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/oauth/import-token", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ImportToken(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestImportToken_ProviderNotFound(t *testing.T) {
	h, _ := newOAuthImportTestDeps(t)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"provider":"missing-provider","access_token":"access-123","refresh_token":"refresh-456","expires_at":1893456000}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/oauth/import-token", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ImportToken(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestImportToken_NonOAuthProvider(t *testing.T) {
	h, _ := newOAuthImportTestDeps(t)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"provider":"openai","access_token":"access-123","refresh_token":"refresh-456","expires_at":1893456000}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/oauth/import-token", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ImportToken(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestImportToken_DeduplicatesByEmail(t *testing.T) {
	h, database := newOAuthImportTestDeps(t)
	now := time.Now().Unix()
	existingID := "existing-grok"
	if _, err := database.Exec(`
		INSERT INTO connections (id, provider_type_id, name, auth_type, oauth_email, oauth_token, oauth_refresh_token, oauth_expires_at, status, is_active, created_at, updated_at)
		VALUES (?, 'grok-cli', 'user@example.com', 'oauth', 'user@example.com', 'old-at', 'old-rt', ?, 'auth_failed', 0, ?, ?)
	`, existingID, now+1000, now, now); err != nil {
		t.Fatalf("seed existing connection: %v", err)
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{
		"provider": "grok-cli",
		"access_token": "access-123",
		"refresh_token": "refresh-456",
		"expires_at": 1893456000,
		"email": "user@example.com"
	}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/oauth/import-token", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ImportToken(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.ID != existingID {
		t.Fatalf("expected existing connection id %q, got %q", existingID, resp.ID)
	}
	if resp.Status != "ready" {
		t.Fatalf("status = %q, want ready", resp.Status)
	}

	var accessTok string
	var isActive int
	if err := database.QueryRow(`SELECT COALESCE(oauth_token,''), is_active FROM connections WHERE id = ?`, existingID).Scan(&accessTok, &isActive); err != nil {
		t.Fatal(err)
	}
	if accessTok != "access-123" {
		t.Errorf("access token = %q, want access-123", accessTok)
	}
	if isActive != 1 {
		t.Errorf("is_active = %d, want 1", isActive)
	}
}

func TestImportToken_CustomProviderRejected(t *testing.T) {
	h, database := newOAuthImportTestDeps(t)
	now := time.Now().Unix()
	if _, err := database.Exec(`
		INSERT INTO provider_types (id, display_name, format, base_url, is_custom, created_at)
		VALUES ('custom-oauth', 'Custom OAuth', 'openai', 'http://x', 1, ?)
	`, now); err != nil {
		t.Fatalf("seed custom provider: %v", err)
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"provider":"custom-oauth","access_token":"access-123","refresh_token":"refresh-456","expires_at":1893456000}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/oauth/import-token", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ImportToken(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}
