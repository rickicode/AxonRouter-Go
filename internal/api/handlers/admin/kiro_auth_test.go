package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/auth"
	"github.com/rickicode/AxonRouter-Go/internal/auth/kiro"
	"github.com/rickicode/AxonRouter-Go/internal/connstate"
)

type stubKiroService struct {
	startLocalServer   func(ctx context.Context, state string) (int, chan *auth.Credentials, error)
	generateAuthURL    func(ctx context.Context, state string) (string, error)
	getUserCode        func(state string) string
	startDeviceFlow    func(ctx context.Context, state, region, startURL, issuerURL, authMethod string) (int, chan *auth.Credentials, error)
	startSocial        func(provider string) (string, string, string, error)
	exchangeSocialCode func(ctx context.Context, sessionID, code string) (*auth.Credentials, error)
	importToken        func(ctx context.Context, refreshToken string) (*auth.Credentials, error)
	validateAPIKey     func(ctx context.Context, apiKey, region string) (*auth.Credentials, error)
	importExternalIDP  func(ctx context.Context, req kiro.ExternalIDPRequest) (*auth.Credentials, error)
	autoImport         func(ctx context.Context) (*kiro.AutoImportResult, error)
}

func (s *stubKiroService) StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error) {
	if s.startLocalServer != nil {
		return s.startLocalServer(ctx, state)
	}
	return 0, nil, nil
}
func (s *stubKiroService) GenerateAuthURL(ctx context.Context, state string) (string, error) {
	if s.generateAuthURL != nil {
		return s.generateAuthURL(ctx, state)
	}
	return "", nil
}
func (s *stubKiroService) GetUserCode(state string) string {
	if s.getUserCode != nil {
		return s.getUserCode(state)
	}
	return ""
}
func (s *stubKiroService) StartDeviceFlow(ctx context.Context, state, region, startURL, issuerURL, authMethod string) (int, chan *auth.Credentials, error) {
	if s.startDeviceFlow != nil {
		return s.startDeviceFlow(ctx, state, region, startURL, issuerURL, authMethod)
	}
	return 0, nil, nil
}
func (s *stubKiroService) StartSocial(provider string) (string, string, string, error) {
	if s.startSocial != nil {
		return s.startSocial(provider)
	}
	return "", "", "", nil
}
func (s *stubKiroService) ExchangeSocialCode(ctx context.Context, sessionID, code string) (*auth.Credentials, error) {
	if s.exchangeSocialCode != nil {
		return s.exchangeSocialCode(ctx, sessionID, code)
	}
	return nil, nil
}
func (s *stubKiroService) ImportToken(ctx context.Context, refreshToken string) (*auth.Credentials, error) {
	if s.importToken != nil {
		return s.importToken(ctx, refreshToken)
	}
	return nil, nil
}
func (s *stubKiroService) ValidateAPIKey(ctx context.Context, apiKey, region string) (*auth.Credentials, error) {
	if s.validateAPIKey != nil {
		return s.validateAPIKey(ctx, apiKey, region)
	}
	return nil, nil
}
func (s *stubKiroService) ImportExternalIDP(ctx context.Context, req kiro.ExternalIDPRequest) (*auth.Credentials, error) {
	if s.importExternalIDP != nil {
		return s.importExternalIDP(ctx, req)
	}
	return nil, nil
}
func (s *stubKiroService) AutoImport(ctx context.Context) (*kiro.AutoImportResult, error) {
	if s.autoImport != nil {
		return s.autoImport(ctx)
	}
	return nil, nil
}

func newKiroAuthTestDeps(t *testing.T) (*KiroAuthHandler, *sql.DB) {
	t.Helper()
	database := newConnectionHandlerTestDB(t)
	store := connstate.NewStore()
	elig := connstate.NewEligibilityManager(store)
	return NewKiroAuthHandler(database, &stubKiroService{}, store, elig), database
}

func TestKiroAuthHandler_ImportKiroToken(t *testing.T) {
	h, database := newKiroAuthTestDeps(t)
	h.svc = &stubKiroService{
		importToken: func(ctx context.Context, refreshToken string) (*auth.Credentials, error) {
			return &auth.Credentials{
				RefreshToken: refreshToken,
				ProviderSpecific: map[string]string{
					"authMethod": "import",
				},
			}, nil
		},
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"refresh_token":"aorAAAAAGimport"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/oauth/kiro/import", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ImportKiroToken(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		ConnectionID string `json:"connection_id"`
		Name         string `json:"name"`
		Status       string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "ready" {
		t.Fatalf("status = %q, want ready", resp.Status)
	}

	var accessTok, refreshTok, psdRaw string
	var expiresAt int64
	err := database.QueryRow(`
		SELECT COALESCE(oauth_token,''), COALESCE(oauth_refresh_token,''), COALESCE(oauth_expires_at,0), COALESCE(provider_specific_data,'')
		FROM connections WHERE id = ?
	`, resp.ConnectionID).Scan(&accessTok, &refreshTok, &expiresAt, &psdRaw)
	if err != nil {
		t.Fatalf("fetch connection: %v", err)
	}
	if refreshTok != "aorAAAAAGimport" {
		t.Fatalf("refresh token = %q, want aorAAAAAGimport", refreshTok)
	}
	var psd map[string]string
	json.Unmarshal([]byte(psdRaw), &psd)
	if psd["authMethod"] != "import" {
		t.Fatalf("authMethod = %q, want import", psd["authMethod"])
	}
	if expiresAt != 0 {
		t.Fatalf("expires_at = %d, want 0", expiresAt)
	}
	if accessTok != "" {
		t.Fatalf("access token should be empty for import, got %q", accessTok)
	}
}

func TestKiroAuthHandler_APIKey(t *testing.T) {
	h, database := newKiroAuthTestDeps(t)
	h.svc = &stubKiroService{
		validateAPIKey: func(ctx context.Context, apiKey, region string) (*auth.Credentials, error) {
			return &auth.Credentials{
				AccessToken: apiKey,
				ProviderSpecific: map[string]string{
					"authMethod": "api_key",
					"profileArn": "arn:aws:codewhisperer:us-east-1:123:profile/apikey",
					"region":     "us-east-1",
				},
			}, nil
		},
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/oauth/kiro/api-key", strings.NewReader(`{"api_key":"key-123"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.APIKey(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var resp struct{ ConnectionID string `json:"connection_id"` }
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}

	var accessTok, psdRaw string
	err := database.QueryRow(`SELECT COALESCE(oauth_token,''), COALESCE(provider_specific_data,'') FROM connections WHERE id = ?`, resp.ConnectionID).Scan(&accessTok, &psdRaw)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if accessTok != "key-123" {
		t.Fatalf("access token = %q", accessTok)
	}
	var psd map[string]string
	json.Unmarshal([]byte(psdRaw), &psd)
	if psd["authMethod"] != "api_key" {
		t.Fatalf("authMethod = %q", psd["authMethod"])
	}
}

func TestKiroAuthHandler_ExternalIDP(t *testing.T) {
	h, database := newKiroAuthTestDeps(t)
	h.svc = &stubKiroService{
		importExternalIDP: func(ctx context.Context, req kiro.ExternalIDPRequest) (*auth.Credentials, error) {
			return &auth.Credentials{
				AccessToken:  req.AccessToken,
				RefreshToken: req.RefreshToken,
				ProviderSpecific: map[string]string{
					"authMethod":    "external_idp",
					"tokenEndpoint": req.TokenEndpoint,
					"tokenType":     "EXTERNAL_IDP",
				},
			}, nil
		},
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{
		"access_token":"access-123",
		"refresh_token":"refresh-456",
		"token_endpoint":"https://login.microsoftonline.com/tenant/oauth2/v2.0/token",
		"client_id":"client-id",
		"scope":"openid offline_access",
		"profile_arn":"arn:aws:codewhisperer:us-east-1:123:profile/ext"
	}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/oauth/kiro/external-idp", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ExternalIDP(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var resp struct{ ConnectionID string `json:"connection_id"` }
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}

	var psdRaw string
	database.QueryRow(`SELECT COALESCE(provider_specific_data,'') FROM connections WHERE id = ?`, resp.ConnectionID).Scan(&psdRaw)
	var psd map[string]string
	json.Unmarshal([]byte(psdRaw), &psd)
	if psd["tokenType"] != "EXTERNAL_IDP" {
		t.Fatalf("tokenType = %q", psd["tokenType"])
	}
}

func TestKiroAuthHandler_SocialCallback(t *testing.T) {
	h, database := newKiroAuthTestDeps(t)
	h.svc = &stubKiroService{
		exchangeSocialCode: func(ctx context.Context, sessionID, code string) (*auth.Credentials, error) {
			return &auth.Credentials{
				AccessToken:  "access-789",
				RefreshToken: "refresh-abc",
				Email:        "user@example.com",
				ProviderSpecific: map[string]string{
					"authMethod": "github",
					"profileArn": "arn:aws:codewhisperer:us-east-1:123:profile/social",
				},
			}, nil
		},
	}

	h.storeSession("sess-1", &kiroAuthSession{method: "github"})

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/oauth/kiro/github/callback", strings.NewReader(`{"session_id":"sess-1","code":"code-1"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.GitHubCallback(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		ConnectionID string `json:"connection_id"`
		Name         string `json:"name"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Name != "user@example.com" {
		t.Fatalf("name = %q", resp.Name)
	}
	var name string
	database.QueryRow(`SELECT name FROM connections WHERE id = ?`, resp.ConnectionID).Scan(&name)
	if name != "user@example.com" {
		t.Fatalf("db name = %q", name)
	}
}

func TestKiroAuthHandler_Poll(t *testing.T) {
	h, _ := newKiroAuthTestDeps(t)
	session := &kiroAuthSession{method: "import", status: "connected", name: "user@example.com", connID: "conn-1"}
	h.storeSession("sess-1", session)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "sessionId", Value: "sess-1"}}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/oauth/kiro/sess-1/poll", nil)
	h.Poll(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Status       string `json:"status"`
		ConnectionID string `json:"connection_id"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "connected" || resp.ConnectionID != "conn-1" {
		t.Fatalf("unexpected poll response: %+v", resp)
	}
}

func TestKiroAuthHandler_StartBuilderID(t *testing.T) {
	h, _ := newKiroAuthTestDeps(t)
	h.svc = &stubKiroService{
		startDeviceFlow: func(ctx context.Context, state, region, startURL, issuerURL, authMethod string) (int, chan *auth.Credentials, error) {
			return 0, make(chan *auth.Credentials), nil
		},
		generateAuthURL: func(ctx context.Context, state string) (string, error) {
			return "https://example.com/verify", nil
		},
		getUserCode: func(state string) string { return "UCODE" },
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/oauth/kiro/builder-id/start", nil)

	h.StartBuilderID(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		AuthURL   string `json:"auth_url"`
		SessionID string `json:"session_id"`
		UserCode  string `json:"user_code"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.AuthURL != "https://example.com/verify" {
		t.Fatalf("auth_url = %q", resp.AuthURL)
	}
	if resp.UserCode != "UCODE" {
		t.Fatalf("user_code = %q", resp.UserCode)
	}
}
