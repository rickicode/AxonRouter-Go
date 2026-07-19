package kiro

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

// rewriteTransport swaps real upstream hosts with test servers.
type rewriteTransport struct {
	base    http.RoundTripper
	rewrite map[string]string // host -> test server URL
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if target, ok := t.rewrite[req.URL.Host]; ok {
		u, _ := url.Parse(target)
		req.URL.Scheme = u.Scheme
		req.URL.Host = u.Host
		req.Host = ""
	}
	return t.base.RoundTrip(req)
}

func testClient(servers map[string]*httptest.Server) *http.Client {
	rewrite := make(map[string]string)
	for host, srv := range servers {
		rewrite[host] = srv.URL
	}
	return &http.Client{
		Transport: &rewriteTransport{base: http.DefaultTransport, rewrite: rewrite},
		Timeout:   10 * time.Second,
	}
}

func authServiceBaseHost() string {
	u, _ := url.Parse(authServiceBase)
	return u.Host
}

func TestNewAuthService_ImplementsOAuthService(t *testing.T) {
	svc := NewAuthService(nil)
	var _ auth.OAuthService = svc
	if svc.httpClient == nil {
		t.Fatal("expected default http client")
	}
}

func TestStartSocial_Google(t *testing.T) {
	svc := NewAuthService(nil)
	authURL, sessionID, _, err := svc.StartSocial("google")
	if err != nil {
		t.Fatalf("start social: %v", err)
	}
	if authURL == "" || sessionID == "" {
		t.Fatal("expected auth URL and session ID")
	}
	if !strings.Contains(authURL, "idp=Google") || !strings.Contains(authURL, "code_challenge=") {
		t.Fatalf("unexpected social URL: %s", authURL)
	}
}

func TestExchangeSocialCode(t *testing.T) {
	socialSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accessToken":  "access-123",
			"refreshToken": "refresh-456",
			"profileArn":   "arn:aws:codewhisperer:us-east-1:123:profile/test",
			"expiresIn":    3600,
		})
	}))
	defer socialSrv.Close()

	svc := NewAuthService(testClient(map[string]*httptest.Server{authServiceBaseHost(): socialSrv}))
	authURL, sessionID, _, err := svc.StartSocial("github")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	u, _ := url.Parse(authURL)
	if u.Query().Get("state") != sessionID {
		t.Fatal("state mismatch")
	}

	creds, err := svc.ExchangeSocialCode(context.Background(), sessionID, "code-xyz")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if creds.AccessToken != "access-123" || creds.ProviderSpecific["authMethod"] != "github" {
		t.Fatalf("unexpected creds: %+v", creds)
	}
}

func TestImportToken(t *testing.T) {
	svc := NewAuthService(nil)
	if _, err := svc.ImportToken(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty token")
	}
	creds, err := svc.ImportToken(context.Background(), "aorAAAAAGvalid")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if creds.ProviderSpecific["authMethod"] != "import" {
		t.Fatalf("authMethod = %q", creds.ProviderSpecific["authMethod"])
	}
}

func TestValidateAPIKey(t *testing.T) {
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-amz-target") != "AmazonCodeWhispererService.ListAvailableProfiles" {
			http.Error(w, "wrong target", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"profiles": []map[string]any{
				{"arn": "arn:aws:codewhisperer:us-east-1:123:profile/default"},
			},
		})
	}))
	defer apiSrv.Close()

	svc := NewAuthService(testClient(map[string]*httptest.Server{
		"codewhisperer.us-east-1.amazonaws.com": apiSrv,
	}))
	creds, err := svc.ValidateAPIKey(context.Background(), "api-key-123", "us-east-1")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if creds.AccessToken != "api-key-123" || creds.ProviderSpecific["profileArn"] == "" {
		t.Fatalf("unexpected creds: %+v", creds)
	}
	if creds.ProviderSpecific["authMethod"] != "api_key" {
		t.Fatalf("authMethod = %q", creds.ProviderSpecific["authMethod"])
	}
}

func TestImportExternalIDP(t *testing.T) {
	svc := NewAuthService(nil)
	_, err := svc.ImportExternalIDP(context.Background(), ExternalIDPRequest{
		TokenEndpoint: "http://evil.com/token",
		ClientID:      "client",
		Scope:         "openid",
	})
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected https error, got %v", err)
	}

	creds, err := svc.ImportExternalIDP(context.Background(), ExternalIDPRequest{
		AccessToken:   "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJlbWFpbCI6InVzZXJAZXhhbXBsZS5jb20ifQ.fake",
		RefreshToken:  "refresh-123",
		TokenEndpoint: "https://login.microsoftonline.com/tenant/oauth2/v2.0/token",
		ClientID:      "client-id",
		Scope:         "api://client-id/codewhisperer:conversations offline_access",
		ProfileArn:    "arn:aws:codewhisperer:us-east-1:123:profile/external",
		Region:        "us-east-1",
	})
	if err != nil {
		t.Fatalf("import external idp: %v", err)
	}
	if creds.ProviderSpecific["authMethod"] != "external_idp" || creds.ProviderSpecific["tokenType"] != "EXTERNAL_IDP" {
		t.Fatalf("unexpected creds: %+v", creds.ProviderSpecific)
	}
	if creds.Email != "user@example.com" {
		t.Fatalf("email = %q", creds.Email)
	}
}

func TestRefreshExternalIDP(t *testing.T) {
	idpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.FormValue("grant_type") != "refresh_token" || r.FormValue("client_id") != "client-id" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	}))
	defer idpSrv.Close()

	svc := NewAuthService(testClient(map[string]*httptest.Server{"login.microsoftonline.com": idpSrv}))
	creds := &auth.Credentials{
		RefreshToken: "old-refresh",
		ProviderSpecific: map[string]string{
			"authMethod":    "external_idp",
			"clientId":      "client-id",
			"tokenEndpoint": "https://login.microsoftonline.com/tenant/oauth2/v2.0/token",
			"scope":         "openid",
		},
	}
	newCreds, err := svc.RefreshToken(context.Background(), creds)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if newCreds.AccessToken != "new-access" {
		t.Fatalf("access token = %q", newCreds.AccessToken)
	}
}

func TestRefreshSocial(t *testing.T) {
	socialSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/refreshToken" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accessToken":  "new-access",
			"refreshToken": "new-refresh",
			"expiresIn":    3600,
		})
	}))
	defer socialSrv.Close()

	svc := NewAuthService(testClient(map[string]*httptest.Server{authServiceBaseHost(): socialSrv}))
	creds := &auth.Credentials{
		RefreshToken: "old-refresh",
		ProviderSpecific: map[string]string{
			"authMethod": "import",
		},
	}
	newCreds, err := svc.RefreshToken(context.Background(), creds)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if newCreds.AccessToken != "new-access" || newCreds.RefreshToken != "new-refresh" {
		t.Fatalf("unexpected creds: %+v", newCreds)
	}
}

func TestRefreshOIDC(t *testing.T) {
	oidcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client/register" {
			_ = json.NewEncoder(w).Encode(map[string]any{"clientId": "new-id", "clientSecret": "new-secret"})
			return
		}
		if r.URL.Path == "/token" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"accessToken":  "new-access",
				"refreshToken": "new-refresh",
				"expiresIn":    3600,
			})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer oidcSrv.Close()

	svc := NewAuthService(testClient(map[string]*httptest.Server{"oidc.us-east-1.amazonaws.com": oidcSrv}))
	creds := &auth.Credentials{
		RefreshToken: "old-refresh",
		ProviderSpecific: map[string]string{
			"authMethod":   "builder-id",
			"clientId":     "old-id",
			"clientSecret": "old-secret",
			"region":       "us-east-1",
			"startUrl":     "https://view.awsapps.com/start",
		},
	}
	newCreds, err := svc.RefreshToken(context.Background(), creds)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if newCreds.AccessToken != "new-access" {
		t.Fatalf("access token = %q", newCreds.AccessToken)
	}
}

func TestStartDeviceFlow(t *testing.T) {
	var registerHits, deviceHits, tokenHits int
	oidcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client/register":
			registerHits++
			_ = json.NewEncoder(w).Encode(map[string]any{"clientId": "cid", "clientSecret": "cs"})
		case "/device_authorization":
			deviceHits++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"deviceCode":      "dc",
				"userCode":        "UCODE",
				"verificationUri": "https://example.com/verify",
			})
		case "/token":
			tokenHits++
			if tokenHits == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"accessToken":  "access-123",
				"refreshToken": "refresh-456",
				"expiresIn":    3600,
			})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer oidcSrv.Close()

	svc := NewAuthService(testClient(map[string]*httptest.Server{"oidc.us-east-1.amazonaws.com": oidcSrv}))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	port, resultChan, err := svc.StartDeviceFlow(ctx, "state1", "us-east-1", "https://start.example.com", "https://issuer.example.com", "idc")
	if err != nil {
		t.Fatalf("start device flow: %v", err)
	}
	if port != 0 {
		t.Fatalf("port = %d, want 0", port)
	}
	authURL, err := svc.GenerateAuthURL(ctx, "state1:0")
	if err != nil || authURL != "https://example.com/verify" {
		t.Fatalf("auth url = %q, err=%v", authURL, err)
	}
	if svc.GetUserCode("state1:0") != "UCODE" {
		t.Fatal("missing user code")
	}

	creds := <-resultChan
	if creds == nil || creds.ProviderSpecific["__oauth_error__"] != "" {
		t.Fatalf("device flow failed: %+v", creds)
	}
	if creds.AccessToken != "access-123" || creds.ProviderSpecific["authMethod"] != "idc" {
		t.Fatalf("unexpected creds: %+v", creds)
	}
	if registerHits != 1 || deviceHits != 1 || tokenHits != 2 {
		t.Fatalf("register=%d device=%d token=%d", registerHits, deviceHits, tokenHits)
	}
}

