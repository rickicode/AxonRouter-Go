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
	importToken        func(ctx context.Context, req kiro.ImportTokenRequest) (*auth.Credentials, error)
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
func (s *stubKiroService) ImportToken(ctx context.Context, req kiro.ImportTokenRequest) (*auth.Credentials, error) {
	if s.importToken != nil {
		return s.importToken(ctx, req)
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
		importToken: func(ctx context.Context, req kiro.ImportTokenRequest) (*auth.Credentials, error) {
			return &auth.Credentials{
				RefreshToken: req.RefreshToken,
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
	if cs := h.store.Get(resp.ConnectionID); cs == nil || cs.Prefix != "kiro" {
		t.Fatalf("expected connection prefix kiro, got cs=%v", cs)
	}
	if ids := h.elig.GetByPrefix("kiro"); !containsString(ids, resp.ConnectionID) {
		t.Fatalf("expected connection %s in kiro eligible list, got %v", resp.ConnectionID, ids)
	}
}

func containsString(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func TestKiroAuthHandler_ImportKiroToken_DeduplicatesByEmail(t *testing.T) {
	h, database := newKiroAuthTestDeps(t)
	now := time.Now().Unix()
	existingID := "existing-kiro"
	if _, err := database.Exec(`
		INSERT INTO connections (id, provider_type_id, name, auth_type, oauth_email, oauth_token, oauth_refresh_token, oauth_expires_at, status, is_active, created_at, updated_at)
		VALUES (?, 'kiro', 'kiro@example.com', 'oauth', 'kiro@example.com', 'old-at', 'old-rt', ?, 'auth_failed', 0, ?, ?)
	`, existingID, now+1000, now, now); err != nil {
		t.Fatalf("seed existing connection: %v", err)
	}

	h.svc = &stubKiroService{
		importToken: func(ctx context.Context, req kiro.ImportTokenRequest) (*auth.Credentials, error) {
			return &auth.Credentials{
				RefreshToken: req.RefreshToken,
				Email:        "kiro@example.com",
				ProviderSpecific: map[string]string{
					"authMethod": "import",
				},
			}, nil
		},
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"refresh_token":"aorAAAAAGnew"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/oauth/kiro/import", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ImportKiroToken(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		ConnectionID string `json:"connection_id"`
		Status       string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ConnectionID != existingID {
		t.Fatalf("expected existing connection id %q, got %q", existingID, resp.ConnectionID)
	}
	if resp.Status != "ready" {
		t.Fatalf("status = %q, want ready", resp.Status)
	}

	var refreshTok string
	var isActive int
	if err := database.QueryRow(`SELECT COALESCE(oauth_refresh_token,''), is_active FROM connections WHERE id = ?`, existingID).Scan(&refreshTok, &isActive); err != nil {
		t.Fatal(err)
	}
	if refreshTok != "aorAAAAAGnew" {
		t.Errorf("refresh token = %q, want aorAAAAAGnew", refreshTok)
	}
	if isActive != 1 {
		t.Errorf("is_active = %d, want 1", isActive)
	}
}

func TestKiroAuthHandler_ImportKiroToken_WithClientCredentials(t *testing.T) {
	h, database := newKiroAuthTestDeps(t)
	var gotReq kiro.ImportTokenRequest
	h.svc = &stubKiroService{
		importToken: func(ctx context.Context, req kiro.ImportTokenRequest) (*auth.Credentials, error) {
			gotReq = req
			return &auth.Credentials{
				RefreshToken: req.RefreshToken,
				ProviderSpecific: map[string]string{
					"authMethod":   "import",
					"clientId":     req.ClientID,
					"clientSecret": req.ClientSecret,
					"region":       req.Region,
					"startUrl":     req.StartURL,
				},
			}, nil
		},
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body := `{"refresh_token":"aorAAAAAGimport","client_id":"client-id","client_secret":"client-secret","region":"us-west-2","start_url":"https://view.awsapps.com/start"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/api/admin/oauth/kiro/import", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	h.ImportKiroToken(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	if gotReq.ClientID != "client-id" {
		t.Fatalf("client_id = %q", gotReq.ClientID)
	}
	if gotReq.ClientSecret != "client-secret" {
		t.Fatalf("client_secret = %q", gotReq.ClientSecret)
	}
	if gotReq.Region != "us-west-2" {
		t.Fatalf("region = %q", gotReq.Region)
	}
	if gotReq.StartURL != "https://view.awsapps.com/start" {
		t.Fatalf("start_url = %q", gotReq.StartURL)
	}

	var resp struct {
		ConnectionID string `json:"connection_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var psdRaw string
	err := database.QueryRow(`SELECT COALESCE(provider_specific_data,'') FROM connections WHERE id = ?`, resp.ConnectionID).Scan(&psdRaw)
	if err != nil {
		t.Fatalf("fetch connection: %v", err)
	}
	var psd map[string]string
	json.Unmarshal([]byte(psdRaw), &psd)
	if psd["clientId"] != "client-id" {
		t.Fatalf("clientId = %q", psd["clientId"])
	}
	if psd["clientSecret"] != "client-secret" {
		t.Fatalf("clientSecret = %q", psd["clientSecret"])
	}
	if psd["region"] != "us-west-2" {
		t.Fatalf("region = %q", psd["region"])
	}
	if psd["startUrl"] != "https://view.awsapps.com/start" {
		t.Fatalf("startUrl = %q", psd["startUrl"])
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
	var resp struct {
		ConnectionID string `json:"connection_id"`
	}
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
	if cs := h.store.Get(resp.ConnectionID); cs == nil || cs.Prefix != "kiro" {
		t.Fatalf("expected connection prefix kiro, got cs=%v", cs)
	}
	if !h.elig.IsEligible(resp.ConnectionID) {
		t.Fatalf("expected connection %s to be eligible", resp.ConnectionID)
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
	var resp struct {
		ConnectionID string `json:"connection_id"`
	}
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
	if cs := h.store.Get(resp.ConnectionID); cs == nil || cs.Prefix != "kiro" {
		t.Fatalf("expected connection prefix kiro, got cs=%v", cs)
	}
	if !h.elig.IsEligible(resp.ConnectionID) {
		t.Fatalf("expected connection %s to be eligible", resp.ConnectionID)
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
	if cs := h.store.Get(resp.ConnectionID); cs == nil || cs.Prefix != "kiro" {
		t.Fatalf("expected connection prefix kiro, got cs=%v", cs)
	}
	if !h.elig.IsEligible(resp.ConnectionID) {
		t.Fatalf("expected connection %s to be eligible", resp.ConnectionID)
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
