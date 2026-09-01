package iflow

import (
	"context"
	"encoding/base64"
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
	svc := NewOAuthService(http.DefaultClient)
	authURL, err := svc.GenerateAuthURL(context.Background(), "randomstate:1234")
	if err != nil {
		t.Fatalf("GenerateAuthURL error: %v", err)
	}

	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}
	if u.Host != "iflow.cn" || u.Path != "/oauth" {
		t.Errorf("unexpected authorize URL: %s", authURL)
	}
	q := u.Query()
	if q.Get("loginMethod") != "phone" {
		t.Errorf("loginMethod = %q, want phone", q.Get("loginMethod"))
	}
	if q.Get("type") != "phone" {
		t.Errorf("type = %q, want phone", q.Get("type"))
	}
	if q.Get("redirect") != "http://127.0.0.1:1234/callback" {
		t.Errorf("redirect = %q", q.Get("redirect"))
	}
	if q.Get("state") != "randomstate" {
		t.Errorf("state = %q, want randomstate", q.Get("state"))
	}
	if q.Get("client_id") != ClientID {
		t.Errorf("client_id = %q, want %q", q.Get("client_id"), ClientID)
	}
}

func TestExchangeCode(t *testing.T) {
	var gotForm url.Values
	var gotAuth string
	var gotUserInfoToken string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			gotAuth = r.Header.Get("Authorization")
			body, _ := io.ReadAll(r.Body)
			gotForm, _ = url.ParseQuery(string(body))
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "if-access-123",
				"refresh_token": "if-refresh-456",
				"expires_in":    3600,
				"token_type":    "Bearer",
			})
		case "/getUserInfo":
			gotUserInfoToken = r.URL.Query().Get("accessToken")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"apiKey":   "ak-iflow",
					"email":    "user@iflow.example",
					"phone":    "+8613800000000",
					"nickname": "IFlowUser",
					"name":     "User Name",
					"userId":   "u-987",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	svc := NewOAuthService(http.DefaultClient)
	svc.testTokenURL = ts.URL + "/token"
	svc.testUserInfoURL = ts.URL + "/getUserInfo"

	creds, err := svc.ExchangeCode(context.Background(), "auth-code-xyz")
	if err != nil {
		t.Fatalf("ExchangeCode error: %v", err)
	}
	if creds.AccessToken != "if-access-123" {
		t.Errorf("AccessToken = %q", creds.AccessToken)
	}
	if creds.RefreshToken != "if-refresh-456" {
		t.Errorf("RefreshToken = %q", creds.RefreshToken)
	}
	if !creds.ExpiresAt.After(time.Now()) {
		t.Error("ExpiresAt not set")
	}
	if creds.ProviderSpecific["api_key"] != "ak-iflow" {
		t.Errorf("api_key = %q", creds.ProviderSpecific["api_key"])
	}
	if creds.ProviderSpecific["email"] != "user@iflow.example" {
		t.Errorf("email = %q", creds.ProviderSpecific["email"])
	}
	if creds.ProviderSpecific["phone"] != "+8613800000000" {
		t.Errorf("phone = %q", creds.ProviderSpecific["phone"])
	}
	if creds.ProviderSpecific["nickname"] != "IFlowUser" {
		t.Errorf("nickname = %q", creds.ProviderSpecific["nickname"])
	}
	if creds.ProviderSpecific["user_id"] != "u-987" {
		t.Errorf("user_id = %q", creds.ProviderSpecific["user_id"])
	}
	if creds.Email != "user@iflow.example" {
		t.Errorf("Email = %q", creds.Email)
	}
	if creds.AccountID != "u-987" {
		t.Errorf("AccountID = %q", creds.AccountID)
	}

	// Basic auth header must be base64(clientId:clientSecret).
	wantBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte(ClientID+":"+ClientSecret))
	if gotAuth != wantBasic {
		t.Errorf("Authorization = %q, want %q", gotAuth, wantBasic)
	}
	if gotForm.Get("grant_type") != "authorization_code" {
		t.Errorf("grant_type = %q", gotForm.Get("grant_type"))
	}
	if gotForm.Get("code") != "auth-code-xyz" {
		t.Errorf("code = %q", gotForm.Get("code"))
	}
	if gotForm.Get("client_id") != ClientID {
		t.Errorf("client_id = %q", gotForm.Get("client_id"))
	}
	if gotForm.Get("client_secret") != ClientSecret {
		t.Errorf("client_secret = %q", gotForm.Get("client_secret"))
	}
	if gotUserInfoToken != "if-access-123" {
		t.Errorf("userinfo accessToken = %q, want %q", gotUserInfoToken, "if-access-123")
	}
}

func TestExchangeCode_UserInfoFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "if-access-123",
				"refresh_token": "if-refresh-456",
				"expires_in":    3600,
			})
		case "/getUserInfo":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"success": false,
				"message": "invalid token",
			})
		}
	}))
	defer ts.Close()

	svc := NewOAuthService(http.DefaultClient)
	svc.testTokenURL = ts.URL + "/token"
	svc.testUserInfoURL = ts.URL + "/getUserInfo"

	if _, err := svc.ExchangeCode(context.Background(), "code"); err == nil {
		t.Fatal("expected error when iFlow user info reports success=false")
	} else if !strings.Contains(err.Error(), "invalid token") {
		t.Errorf("error = %v, want userinfo message", err)
	}
}

func TestExchangeCode_MissingAPIKey(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "if-access-123",
				"refresh_token": "if-refresh-456",
				"expires_in":    3600,
			})
		case "/getUserInfo":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"email": "user@iflow.example",
				},
			})
		}
	}))
	defer ts.Close()

	svc := NewOAuthService(http.DefaultClient)
	svc.testTokenURL = ts.URL + "/token"
	svc.testUserInfoURL = ts.URL + "/getUserInfo"

	if _, err := svc.ExchangeCode(context.Background(), "code"); err == nil {
		t.Fatal("expected error when apiKey is missing")
	} else if !strings.Contains(err.Error(), "empty API key") {
		t.Errorf("error = %v, want empty API key error", err)
	}
}

func TestRefreshToken(t *testing.T) {
	var gotForm url.Values
	var gotAuth string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    7200,
		})
	}))
	defer ts.Close()

	svc := NewOAuthService(http.DefaultClient)
	svc.testTokenURL = ts.URL + "/token"

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
	if gotForm.Get("grant_type") != "refresh_token" {
		t.Errorf("grant_type = %q", gotForm.Get("grant_type"))
	}
	if gotForm.Get("refresh_token") != "old-refresh" {
		t.Errorf("refresh_token = %q", gotForm.Get("refresh_token"))
	}
	if gotForm.Get("client_id") != ClientID {
		t.Errorf("client_id = %q", gotForm.Get("client_id"))
	}
	wantBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte(ClientID+":"+ClientSecret))
	if gotAuth != wantBasic {
		t.Errorf("Authorization = %q, want %q", gotAuth, wantBasic)
	}
}

func TestStartLocalServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
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
		case "/getUserInfo":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"apiKey": "ak-local",
					"email":  "local@iflow.example",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	svc := NewOAuthService(http.DefaultClient)
	svc.testTokenURL = ts.URL + "/token"
	svc.testUserInfoURL = ts.URL + "/getUserInfo"

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
		if creds.ProviderSpecific["api_key"] != "ak-local" {
			t.Errorf("api_key = %q", creds.ProviderSpecific["api_key"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for credentials")
	}
}

func TestStateParam(t *testing.T) {
	if got := stateParam("randomstate:1234"); got != "randomstate" {
		t.Errorf("stateParam = %q", got)
	}
	if got := stateParam("nostate"); got != "nostate" {
		t.Errorf("stateParam = %q", got)
	}
}

func TestPortFromState(t *testing.T) {
	if got := portFromState("state:4567", 1455); got != 4567 {
		t.Errorf("portFromState(state:4567) = %d", got)
	}
	if got := portFromState("state", 1455); got != 1455 {
		t.Errorf("portFromState(state) = %d, want default", got)
	}
}
