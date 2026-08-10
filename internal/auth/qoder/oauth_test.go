package qoder

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

func TestGenerateAuthURL(t *testing.T) {
	t.Setenv("QODER_OAUTH_AUTHORIZE_URL", "https://qoder.example.com/oauth/authorize")
	t.Setenv("QODER_OAUTH_CLIENT_ID", "qoder-client-id")

	svc := NewOAuthService(http.DefaultClient)
	authURL, err := svc.GenerateAuthURL(context.Background(), "randomstate:1234")
	if err != nil {
		t.Fatalf("GenerateAuthURL error: %v", err)
	}

	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}
	q := u.Query()
	if q.Get("client_id") != "qoder-client-id" {
		t.Errorf("client_id = %q, want %q", q.Get("client_id"), "qoder-client-id")
	}
	if q.Get("redirect_uri") != "http://127.0.0.1:1234/callback" {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("state") != "randomstate" {
		t.Errorf("state = %q", q.Get("state"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q", q.Get("response_type"))
	}
	if q.Get("loginMethod") != "" {
		t.Errorf("unexpected loginMethod = %q", q.Get("loginMethod"))
	}
}

func TestGenerateAuthURL_PhoneAuth(t *testing.T) {
	t.Setenv("QODER_OAUTH_AUTHORIZE_URL", "https://qoder.example.com/oauth/authorize")
	t.Setenv("QODER_OAUTH_CLIENT_ID", "qoder-client-id")

	svc := NewOAuthService(http.DefaultClient)
	svc.UsePhoneAuth = true
	authURL, err := svc.GenerateAuthURL(context.Background(), "state:5678")
	if err != nil {
		t.Fatalf("GenerateAuthURL error: %v", err)
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}
	q := u.Query()
	if q.Get("loginMethod") != "phone" {
		t.Errorf("loginMethod = %q, want phone", q.Get("loginMethod"))
	}
	if q.Get("type") != "phone" {
		t.Errorf("type = %q, want phone", q.Get("type"))
	}
}

func TestExchangeCode(t *testing.T) {
	tokenCalls := 0
	var gotAuth string
	var gotForm url.Values
	var gotUserInfoToken string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls++
			gotAuth = r.Header.Get("Authorization")
			body, _ := io.ReadAll(r.Body)
			gotForm, _ = url.ParseQuery(string(body))
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access-123",
				"refresh_token": "refresh-456",
				"expires_in":    3600,
				"token_type":    "Bearer",
			})
		case "/userinfo":
			gotUserInfoToken = r.URL.Query().Get("accessToken")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"apiKey":   "ak-qoder",
					"email":    "qoder@example.com",
					"nickname": "QoderUser",
					"phone":    "+123",
				},
			})
		}
	}))
	defer ts.Close()

	t.Setenv("QODER_OAUTH_TOKEN_URL", ts.URL+"/token")
	t.Setenv("QODER_OAUTH_USERINFO_URL", ts.URL+"/userinfo")
	t.Setenv("QODER_OAUTH_CLIENT_ID", "client-id")
	t.Setenv("QODER_OAUTH_CLIENT_SECRET", "client-secret")

	svc := NewOAuthService(http.DefaultClient)
	svc.statePorts["mystate"] = 1234
	creds, err := svc.ExchangeCode(context.Background(), "auth-code-xyz")
	if err != nil {
		t.Fatalf("ExchangeCode error: %v", err)
	}
	if creds.AccessToken != "access-123" {
		t.Errorf("AccessToken = %q, want %q", creds.AccessToken, "access-123")
	}
	if creds.RefreshToken != "refresh-456" {
		t.Errorf("RefreshToken = %q, want %q", creds.RefreshToken, "refresh-456")
	}
	if !creds.ExpiresAt.After(time.Now()) {
		t.Errorf("ExpiresAt not set")
	}
	if creds.ProviderSpecific["api_key"] != "ak-qoder" {
		t.Errorf("api_key = %q", creds.ProviderSpecific["api_key"])
	}
	if creds.ProviderSpecific["email"] != "qoder@example.com" {
		t.Errorf("email = %q", creds.ProviderSpecific["email"])
	}
	if creds.ProviderSpecific["nickname"] != "QoderUser" {
		t.Errorf("nickname = %q", creds.ProviderSpecific["nickname"])
	}
	if creds.ProviderSpecific["phone"] != "+123" {
		t.Errorf("phone = %q", creds.ProviderSpecific["phone"])
	}
	if creds.Email != "qoder@example.com" {
		t.Errorf("Email field = %q", creds.Email)
	}
	if tokenCalls != 1 {
		t.Errorf("token endpoint called %d times, want 1", tokenCalls)
	}
	if gotForm.Get("grant_type") != "authorization_code" {
		t.Errorf("grant_type = %q", gotForm.Get("grant_type"))
	}
	if gotForm.Get("code") != "auth-code-xyz" {
		t.Errorf("code = %q", gotForm.Get("code"))
	}
	if gotForm.Get("client_id") != "client-id" {
		t.Errorf("client_id = %q", gotForm.Get("client_id"))
	}
	if gotForm.Get("client_secret") != "client-secret" {
		t.Errorf("client_secret = %q", gotForm.Get("client_secret"))
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Errorf("expected Basic auth, got %q", gotAuth)
	}
	if gotUserInfoToken != "access-123" {
		t.Errorf("userinfo accessToken = %q, want %q", gotUserInfoToken, "access-123")
	}
}

func TestExchangeCode_NoSecret(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if form.Get("client_secret") != "" {
			t.Errorf("client_secret sent but not configured")
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-no-secret",
			"refresh_token": "refresh-no-secret",
			"expires_in":    3600,
		})
	}))
	defer ts.Close()

	t.Setenv("QODER_OAUTH_TOKEN_URL", ts.URL+"/token")
	t.Setenv("QODER_OAUTH_USERINFO_URL", ts.URL+"/userinfo")
	t.Setenv("QODER_OAUTH_CLIENT_ID", "client-id")

	svc := NewOAuthService(http.DefaultClient)
	svc.statePorts["st"] = 1455
	creds, err := svc.ExchangeCode(context.Background(), "code")
	if err != nil {
		t.Fatalf("ExchangeCode error: %v", err)
	}
	if creds.AccessToken != "access-no-secret" {
		t.Errorf("AccessToken = %q", creds.AccessToken)
	}
}

func TestRefreshToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if form.Get("grant_type") != "refresh_token" {
			t.Errorf("grant_type = %q", form.Get("grant_type"))
		}
		if form.Get("refresh_token") != "old-refresh" {
			t.Errorf("refresh_token = %q", form.Get("refresh_token"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    7200,
		})
	}))
	defer ts.Close()

	t.Setenv("QODER_OAUTH_TOKEN_URL", ts.URL+"/token")
	t.Setenv("QODER_OAUTH_CLIENT_ID", "client-id")
	t.Setenv("QODER_OAUTH_CLIENT_SECRET", "client-secret")

	svc := NewOAuthService(http.DefaultClient)
	creds, err := svc.RefreshToken(context.Background(), &auth.Credentials{RefreshToken: "old-refresh"})
	if err != nil {
		t.Fatalf("RefreshToken error: %v", err)
	}
	if creds.AccessToken != "new-access" {
		t.Errorf("AccessToken = %q", creds.AccessToken)
	}
	if creds.RefreshToken != "new-refresh" {
		t.Errorf("RefreshToken = %q", creds.RefreshToken)
	}
}

func TestStartLocalServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			body, _ := io.ReadAll(r.Body)
			form, _ := url.ParseQuery(string(body))
			if form.Get("code") != "local-code" {
				t.Errorf("code = %q, want %q", form.Get("code"), "local-code")
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "local-access",
				"refresh_token": "local-refresh",
				"expires_in":    3600,
			})
		}
	}))
	defer ts.Close()

	t.Setenv("QODER_OAUTH_TOKEN_URL", ts.URL+"/token")
	t.Setenv("QODER_OAUTH_CLIENT_ID", "client-id")

	svc := NewOAuthService(http.DefaultClient)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	port, resultChan, err := svc.StartLocalServer(ctx, "srv-state")
	if err != nil {
		t.Fatalf("StartLocalServer error: %v", err)
	}
	if port == 0 {
		t.Fatal("expected non-zero port")
	}

	callbackURL := "http://127.0.0.1:" + strconv.Itoa(port) + "/callback?code=local-code&state=srv-state"
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("callback request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("callback status = %d", resp.StatusCode)
	}

	select {
	case creds := <-resultChan:
		if creds == nil {
			t.Fatal("nil credentials")
		}
		if creds.AccessToken != "local-access" {
			t.Errorf("AccessToken = %q", creds.AccessToken)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for credentials")
	}
}


