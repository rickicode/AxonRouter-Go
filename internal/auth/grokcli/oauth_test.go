package grokcli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

func fakeIDToken(email, sub string) string {
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]string{"email": email, "sub": sub})
	return hdr + "." + base64.RawURLEncoding.EncodeToString(claims) + ".sig"
}

func TestNewOAuthServiceUsesDefaultTimeout(t *testing.T) {
	svc := NewOAuthService(nil)
	if svc.httpClient.Timeout != httpClientTimeout {
		t.Fatalf("http client timeout = %v, want %v", svc.httpClient.Timeout, httpClientTimeout)
	}
}

func TestExchangeCodeNotSupported(t *testing.T) {
	svc := NewOAuthService(nil)
	_, err := svc.ExchangeCode(context.Background(), "some-code")
	if err == nil {
		t.Fatal("expected ExchangeCode to return error")
	}
	if !strings.Contains(err.Error(), "device-code flow") {
		t.Errorf("error = %q, expected mention of device-code flow", err.Error())
	}
}

func TestDiscoverResolvesEndpoints(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"device_authorization_endpoint": "https://auth.x.ai/oauth2/device",
			"token_endpoint":                "https://auth.x.ai/oauth2/token",
		})
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	svc.discoveryURL = ts.URL + "/.well-known/openid-configuration"
	d, err := svc.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover error = %v", err)
	}
	if d.DeviceAuthorizationEndpoint != "https://auth.x.ai/oauth2/device" {
		t.Errorf("device auth endpoint = %q", d.DeviceAuthorizationEndpoint)
	}
	if d.TokenEndpoint != "https://auth.x.ai/oauth2/token" {
		t.Errorf("token endpoint = %q", d.TokenEndpoint)
	}
}

func TestDiscoverRejectsNonXAIHost(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"device_authorization_endpoint": "https://auth.evil.example/oauth2/device",
			"token_endpoint":                "https://auth.x.ai/oauth2/token",
		})
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	svc.discoveryURL = ts.URL + "/.well-known/openid-configuration"
	_, err := svc.Discover(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not on x.ai") {
		t.Fatalf("expected non-x.ai error, got %v", err)
	}
}

func TestRequestDeviceCodePostsClientIDAndScope(t *testing.T) {
	var gotForm url.Values
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		_ = r.ParseForm()
		gotForm = r.PostForm
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":               "device-abc",
			"user_code":                 "ABCD-1234",
			"verification_uri":          "https://accounts.x.ai/device",
			"verification_uri_complete": "https://accounts.x.ai/device?user_code=ABCD-1234",
			"expires_in":                1800,
			"interval":                  5,
		})
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	dc, err := svc.requestDeviceCode(context.Background(), ts.URL, "https://auth.x.ai/oauth2/token")
	if err != nil {
		t.Fatalf("requestDeviceCode error = %v", err)
	}
	if dc.DeviceCode != "device-abc" || dc.UserCode != "ABCD-1234" {
		t.Errorf("unexpected device code response: %+v", dc)
	}
	if gotForm.Get("client_id") != ClientID {
		t.Errorf("client_id = %q, want %q", gotForm.Get("client_id"), ClientID)
	}
	if gotForm.Get("scope") != Scope {
		t.Errorf("scope = %q, want %q", gotForm.Get("scope"), Scope)
	}
}

func TestRequestDeviceCodeRequiresVerificationURI(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code": "device-abc",
			"user_code":   "ABCD-1234",
			"expires_in":  1800,
			"interval":    5,
		})
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	_, err := svc.requestDeviceCode(context.Background(), ts.URL, "https://auth.x.ai/oauth2/token")
	if err == nil || !strings.Contains(err.Error(), "verification URI") {
		t.Fatalf("expected verification URI error, got %v", err)
	}
}

func TestPollForTokenExchangesDeviceCode(t *testing.T) {
	var pollCount int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if got := r.PostForm.Get("grant_type"); got != DeviceCodeGrantType {
			t.Fatalf("grant_type = %q, want %q", got, DeviceCodeGrantType)
		}
		if got := r.PostForm.Get("device_code"); got != "device-abc" {
			t.Fatalf("device_code = %q, want device-abc", got)
		}
		if got := r.PostForm.Get("client_id"); got != ClientID {
			t.Fatalf("client_id = %q, want %q", got, ClientID)
		}
		count := atomic.AddInt32(&pollCount, 1)
		w.Header().Set("Content-Type", "application/json")
		if count == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-1",
			"refresh_token": "refresh-1",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"id_token":      fakeIDToken("user@x.ai", "sub-1"),
		})
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	svc.testMinPollInterval = 10 * time.Millisecond
	creds, err := svc.pollForToken(context.Background(), &DeviceCodeResponse{
		DeviceCode:    "device-abc",
		UserCode:      "ABCD-1234",
		ExpiresIn:     60,
		Interval:      1,
		TokenEndpoint: ts.URL,
	})
	if err != nil {
		t.Fatalf("pollForToken error = %v", err)
	}
	if creds.AccessToken != "access-1" || creds.RefreshToken != "refresh-1" {
		t.Errorf("unexpected tokens: %+v", creds)
	}
	if creds.Email != "user@x.ai" || creds.AccountID != "sub-1" {
		t.Errorf("identity = %q / %q, want user@x.ai / sub-1", creds.Email, creds.AccountID)
	}
	if creds.ProviderSpecific["token_endpoint"] != ts.URL {
		t.Errorf("token_endpoint = %q, want %q", creds.ProviderSpecific["token_endpoint"], ts.URL)
	}
	if atomic.LoadInt32(&pollCount) != 2 {
		t.Errorf("poll count = %d, want 2", atomic.LoadInt32(&pollCount))
	}
}

func TestPollForTokenAccessDenied(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "access_denied"})
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	svc.testMinPollInterval = 10 * time.Millisecond
	_, err := svc.pollForToken(context.Background(), &DeviceCodeResponse{
		DeviceCode:    "device-abc",
		UserCode:      "ABCD-1234",
		ExpiresIn:     60,
		Interval:      1,
		TokenEndpoint: ts.URL,
	})
	if err == nil || !strings.Contains(err.Error(), "authorization denied") {
		t.Fatalf("expected authorization denied error, got %v", err)
	}
}

func TestPollForTokenSlowDownContinuesPolling(t *testing.T) {
	var pollCount int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&pollCount, 1)
		w.Header().Set("Content-Type", "application/json")
		if count == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "slow_down"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-slow",
			"refresh_token": "refresh-slow",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	svc.testMinPollInterval = 10 * time.Millisecond
	creds, err := svc.pollForToken(context.Background(), &DeviceCodeResponse{
		DeviceCode:    "device-abc",
		UserCode:      "ABCD-1234",
		ExpiresIn:     60,
		Interval:      1,
		TokenEndpoint: ts.URL,
	})
	if err != nil {
		t.Fatalf("pollForToken error = %v", err)
	}
	if creds.AccessToken != "access-slow" {
		t.Errorf("access token = %q, want access-slow", creds.AccessToken)
	}
	if atomic.LoadInt32(&pollCount) != 2 {
		t.Errorf("poll count = %d, want 2", atomic.LoadInt32(&pollCount))
	}
}

func TestPollForTokenExpired(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "expired_token"})
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	svc.testMinPollInterval = 10 * time.Millisecond
	_, err := svc.pollForToken(context.Background(), &DeviceCodeResponse{
		DeviceCode:    "device-abc",
		UserCode:      "ABCD-1234",
		ExpiresIn:     60,
		Interval:      1,
		TokenEndpoint: ts.URL,
	})
	if err == nil || !strings.Contains(err.Error(), "device code expired") {
		t.Fatalf("expected expired error, got %v", err)
	}
}

func TestRefreshTokenPostsGrantTypeAndStoresEndpoint(t *testing.T) {
	var gotForm url.Values
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"token_type":    "Bearer",
			"expires_in":    3600,
			"id_token":      fakeIDToken("refreshed@x.ai", "sub-r"),
		})
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	creds := &auth.Credentials{
		RefreshToken: "old-refresh",
		ProviderSpecific: map[string]string{
			"token_endpoint": ts.URL,
		},
	}
	newCreds, err := svc.RefreshToken(context.Background(), creds)
	if err != nil {
		t.Fatalf("RefreshToken error = %v", err)
	}
	if newCreds.AccessToken != "new-access" || newCreds.RefreshToken != "new-refresh" {
		t.Errorf("unexpected tokens: %+v", newCreds)
	}
	if newCreds.Email != "refreshed@x.ai" || newCreds.AccountID != "sub-r" {
		t.Errorf("identity = %q / %q, want refreshed@x.ai / sub-r", newCreds.Email, newCreds.AccountID)
	}
	if gotForm.Get("grant_type") != "refresh_token" {
		t.Errorf("grant_type = %q, want refresh_token", gotForm.Get("grant_type"))
	}
	if gotForm.Get("client_id") != ClientID {
		t.Errorf("client_id = %q, want %q", gotForm.Get("client_id"), ClientID)
	}
	if gotForm.Get("refresh_token") != "old-refresh" {
		t.Errorf("refresh_token = %q, want old-refresh", gotForm.Get("refresh_token"))
	}
	if newCreds.ProviderSpecific["token_endpoint"] != ts.URL {
		t.Errorf("token_endpoint = %q, want %q", newCreds.ProviderSpecific["token_endpoint"], ts.URL)
	}
}

func TestRefreshTokenFailsWithoutRefreshToken(t *testing.T) {
	svc := NewOAuthService(nil)
	_, err := svc.RefreshToken(context.Background(), &auth.Credentials{})
	if err == nil || !strings.Contains(err.Error(), "no refresh token") {
		t.Fatalf("expected no refresh token error, got %v", err)
	}
}

func TestGenerateAuthURLReturnsVerificationURI(t *testing.T) {
	svc := NewOAuthService(nil)
	svc.states["state-xyz"] = &deviceFlowState{
		userCode:        "UC-XYZ",
		verificationURI: "https://accounts.x.ai/device",
	}
	url, err := svc.GenerateAuthURL(context.Background(), "state-xyz:0")
	if err != nil {
		t.Fatalf("GenerateAuthURL error = %v", err)
	}
	if url != "https://accounts.x.ai/device" {
		t.Errorf("auth URL = %q, want https://accounts.x.ai/device", url)
	}
}

func TestGenerateAuthURLFailsForUnknownState(t *testing.T) {
	svc := NewOAuthService(nil)
	_, err := svc.GenerateAuthURL(context.Background(), "unknown:0")
	if err == nil {
		t.Fatal("expected error for unknown state")
	}
}

func TestGetUserCode(t *testing.T) {
	svc := NewOAuthService(nil)
	svc.states["state-xyz"] = &deviceFlowState{
		userCode:        "UC-XYZ",
		verificationURI: "https://accounts.x.ai/device",
	}
	if got := svc.GetUserCode("state-xyz:0"); got != "UC-XYZ" {
		t.Errorf("GetUserCode = %q, want UC-XYZ", got)
	}
	if got := svc.GetUserCode("unknown:0"); got != "" {
		t.Errorf("GetUserCode(unknown) = %q, want empty", got)
	}
}

func TestStartLocalServerRunsDeviceFlow(t *testing.T) {
	var discoveryHits int32
	var tokenHits int32
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			atomic.AddInt32(&discoveryHits, 1)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"device_authorization_endpoint": ts.URL + "/device",
				"token_endpoint":                ts.URL + "/token",
			})
		case "/device":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":               "device-abc",
				"user_code":                 "ABCD-1234",
				"verification_uri":          "https://accounts.x.ai/device",
				"verification_uri_complete": "https://accounts.x.ai/device?user_code=ABCD-1234",
				"expires_in":                60,
				"interval":                  1,
			})
		case "/token":
			count := atomic.AddInt32(&tokenHits, 1)
			w.Header().Set("Content-Type", "application/json")
			if count == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access-ok",
				"refresh_token": "refresh-ok",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"id_token":      fakeIDToken("flow@x.ai", "sub-flow"),
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	svc.testDiscoveryResponse = &Discovery{
		DeviceAuthorizationEndpoint: ts.URL + "/device",
		TokenEndpoint:               ts.URL + "/token",
	}
	svc.testMinPollInterval = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	port, resultChan, err := svc.StartLocalServer(ctx, "state-flow:0")
	if err != nil {
		t.Fatalf("StartLocalServer error = %v", err)
	}
	if port != 0 {
		t.Errorf("port = %d, want 0", port)
	}

	url, err := svc.GenerateAuthURL(ctx, "state-flow:0")
	if err != nil {
		t.Fatalf("GenerateAuthURL error = %v", err)
	}
	if url != "https://accounts.x.ai/device" {
		t.Errorf("auth URL = %q", url)
	}
	if got := svc.GetUserCode("state-flow:0"); got != "ABCD-1234" {
		t.Errorf("GetUserCode = %q, want ABCD-1234", got)
	}

	var creds *auth.Credentials
	select {
	case creds = <-resultChan:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for credentials")
	}

	if creds == nil {
		t.Fatal("expected credentials, got nil")
	}
	if creds.AccessToken != "access-ok" {
		t.Errorf("access token = %q, want access-ok", creds.AccessToken)
	}
	if creds.Email != "flow@x.ai" || creds.AccountID != "sub-flow" {
		t.Errorf("identity = %q / %q, want flow@x.ai / sub-flow", creds.Email, creds.AccountID)
	}
	if atomic.LoadInt32(&tokenHits) != 2 {
		t.Errorf("token hits = %d, want 2", atomic.LoadInt32(&tokenHits))
	}
	if svc.GetUserCode("state-flow:0") != "" {
		t.Error("expected state to be cleaned up after flow completes")
	}
}

func TestStartLocalServerSendsOAuthErrorOnFailure(t *testing.T) {
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"device_authorization_endpoint": ts.URL + "/device",
				"token_endpoint":                ts.URL + "/token",
			})
		case "/device":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "device-abc",
				"user_code":        "ABCD-1234",
				"verification_uri": "https://accounts.x.ai/device",
				"expires_in":       60,
				"interval":         1,
			})
		case "/token":
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "access_denied"})
		}
	}))
	defer ts.Close()

	svc := NewOAuthService(ts.Client())
	svc.testDiscoveryResponse = &Discovery{
		DeviceAuthorizationEndpoint: ts.URL + "/device",
		TokenEndpoint:               ts.URL + "/token",
	}
	svc.testMinPollInterval = 10 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, resultChan, err := svc.StartLocalServer(ctx, "state-error:0")
	if err != nil {
		t.Fatalf("StartLocalServer error = %v", err)
	}

	var creds *auth.Credentials
	select {
	case creds = <-resultChan:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for error credential")
	}

	if creds == nil {
		t.Fatal("expected error credential, got nil")
	}
	if creds.ProviderSpecific["__oauth_error__"] == "" {
		t.Fatalf("expected __oauth_error__, got credential: %+v", creds)
	}
}
