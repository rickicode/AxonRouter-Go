package xai

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

func makeIDToken(email, sub string) string {
	payload, _ := json.Marshal(map[string]string{"sub": sub, "email": email})
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func TestDiscover_Fallback(t *testing.T) {
	svc := NewOAuthService(http.DefaultClient)
	svc.discoveryURL = "http://127.0.0.1:1/does-not-exist" // connection refused
	d := svc.Discover(context.Background())
	if d.AuthorizationEndpoint != fallbackAuthEndpoint {
		t.Errorf("fallback auth endpoint = %q", d.AuthorizationEndpoint)
	}
	if d.TokenEndpoint != fallbackTokenEndpoint {
		t.Errorf("fallback token endpoint = %q", d.TokenEndpoint)
	}
	if d.Issuer != Issuer {
		t.Errorf("fallback issuer = %q", d.Issuer)
	}
}

func TestGenerateAuthURL(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Discovery{
			AuthorizationEndpoint: "https://auth.x.ai/oauth2/authorize",
			TokenEndpoint:         "https://auth.x.ai/oauth2/token",
			Issuer:                Issuer,
		})
	}))
	defer ts.Close()

	svc := NewOAuthService(http.DefaultClient)
	svc.discoveryURL = ts.URL
	authURL, err := svc.GenerateAuthURL(context.Background(), "randomstate:1234")
	if err != nil {
		t.Fatalf("GenerateAuthURL error: %v", err)
	}

	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}
	if u.Host != "auth.x.ai" || u.Path != "/oauth2/authorize" {
		t.Errorf("unexpected authorize URL: %s", authURL)
	}
	q := u.Query()
	if q.Get("client_id") != ClientID {
		t.Errorf("client_id = %q, want %q", q.Get("client_id"), ClientID)
	}
	if q.Get("redirect_uri") != RedirectURI {
		t.Errorf("redirect_uri = %q, want %q", q.Get("redirect_uri"), RedirectURI)
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q", q.Get("response_type"))
	}
	if q.Get("scope") != Scope {
		t.Errorf("scope = %q", q.Get("scope"))
	}
	if q.Get("state") != "randomstate" {
		t.Errorf("state = %q, want randomstate", q.Get("state"))
	}
	if q.Get("nonce") == "" {
		t.Error("nonce missing")
	}
	if q.Get("plan") != "generic" {
		t.Errorf("plan = %q, want generic", q.Get("plan"))
	}
	if q.Get("referrer") != "cli-proxy-api" {
		t.Errorf("referrer = %q, want cli-proxy-api", q.Get("referrer"))
	}
	if q.Get("code_challenge") == "" {
		t.Error("code_challenge missing")
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
}

func TestExchangeCode(t *testing.T) {
	var gotForm url.Values

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "xai-access-123",
			"refresh_token": "xai-refresh-456",
			"id_token":      makeIDToken("user@x.ai", "xai-sub-1"),
			"expires_in":    3600,
			"token_type":    "Bearer",
			"scope":         Scope,
		})
	}))
	defer ts.Close()

	svc := NewOAuthService(http.DefaultClient)
	svc.testDiscoveryResponse = &Discovery{
		AuthorizationEndpoint: "https://auth.x.ai/oauth2/authorize",
		TokenEndpoint:         ts.URL + "/token",
		Issuer:                Issuer,
	}

	creds, err := svc.ExchangeCode(context.Background(), "auth-code-xyz")
	if err != nil {
		t.Fatalf("ExchangeCode error: %v", err)
	}
	if creds.AccessToken != "xai-access-123" {
		t.Errorf("AccessToken = %q", creds.AccessToken)
	}
	if creds.RefreshToken != "xai-refresh-456" {
		t.Errorf("RefreshToken = %q", creds.RefreshToken)
	}
	if creds.IDToken == "" {
		t.Error("IDToken not set")
	}
	if creds.Email != "user@x.ai" {
		t.Errorf("Email = %q", creds.Email)
	}
	if creds.AccountID != "xai-sub-1" {
		t.Errorf("AccountID = %q", creds.AccountID)
	}
	if creds.ProviderSpecific["email"] != "user@x.ai" {
		t.Errorf("psd email = %q", creds.ProviderSpecific["email"])
	}
	if creds.ProviderSpecific["sub"] != "xai-sub-1" {
		t.Errorf("psd sub = %q", creds.ProviderSpecific["sub"])
	}
	if creds.ProviderSpecific["idToken"] == "" {
		t.Error("psd idToken not set")
	}
	if !strings.Contains(creds.ProviderSpecific["token_endpoint"], ts.URL) {
		t.Errorf("token_endpoint = %q", creds.ProviderSpecific["token_endpoint"])
	}
	if !creds.ExpiresAt.After(time.Now()) {
		t.Error("ExpiresAt not set")
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
}

func TestRefreshToken(t *testing.T) {
	var gotForm url.Values
	var gotPath string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"id_token":      makeIDToken("new@x.ai", "xai-sub-2"),
			"expires_in":    7200,
		})
	}))
	defer ts.Close()

	svc := NewOAuthService(http.DefaultClient)
	creds, err := svc.RefreshToken(context.Background(), &auth.Credentials{
		RefreshToken: "old-refresh",
		ProviderSpecific: map[string]string{
			"token_endpoint": ts.URL + "/token",
		},
	})
	if err != nil {
		t.Fatalf("RefreshToken error: %v", err)
	}
	if creds.AccessToken != "new-access" {
		t.Errorf("AccessToken = %q", creds.AccessToken)
	}
	if creds.RefreshToken != "new-refresh" {
		t.Errorf("RefreshToken = %q", creds.RefreshToken)
	}
	if creds.Email != "new@x.ai" {
		t.Errorf("Email = %q", creds.Email)
	}
	if creds.AccountID != "xai-sub-2" {
		t.Errorf("AccountID = %q", creds.AccountID)
	}
	if gotPath != "/token" {
		t.Errorf("token path = %q, want /token (PSD endpoint used)", gotPath)
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
}

func TestStartLocalServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if form.Get("code") != "local-code" {
			t.Errorf("code = %q, want %q", form.Get("code"), "local-code")
		}
		if form.Get("code_verifier") == "" {
			t.Error("code_verifier missing on callback exchange")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "local-access",
			"refresh_token": "local-refresh",
			"id_token":      makeIDToken("local@x.ai", "xai-local"),
			"expires_in":    3600,
		})
	}))
	defer ts.Close()

	svc := NewOAuthService(http.DefaultClient)
	svc.testDiscoveryResponse = &Discovery{
		AuthorizationEndpoint: "https://auth.x.ai/oauth2/authorize",
		TokenEndpoint:         ts.URL + "/token",
		Issuer:                Issuer,
	}

	// GenerateAuthURL stores the PKCE verifier for "srv-state".
	if _, err := svc.GenerateAuthURL(context.Background(), "srv-state"); err != nil {
		t.Fatalf("GenerateAuthURL error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	port, resultChan, err := svc.StartLocalServer(ctx, "srv-state")
	if err != nil {
		if strings.Contains(err.Error(), "in use") || strings.Contains(err.Error(), "address already in use") {
			t.Skipf("port %d in use, skipping local server test: %v", LoopbackPort, err)
		}
		t.Fatalf("StartLocalServer error: %v", err)
	}
	if port != LoopbackPort {
		t.Errorf("port = %d, want fixed %d", port, LoopbackPort)
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

func TestExtractTokenClaims(t *testing.T) {
	email, sub := extractTokenClaims(makeIDToken("claims@x.ai", "claims-sub"))
	if email != "claims@x.ai" {
		t.Errorf("email = %q", email)
	}
	if sub != "claims-sub" {
		t.Errorf("sub = %q", sub)
	}

	// Invalid JWT payloads must not panic and must return empty values.
	if e, s := extractTokenClaims(""); e != "" || s != "" {
		t.Errorf("empty token: email=%q sub=%q", e, s)
	}
	if e, s := extractTokenClaims("a.b"); e != "" || s != "" {
		t.Errorf("invalid payload: email=%q sub=%q", e, s)
	}
	if e, s := extractTokenClaims("h." + base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".s"); e != "" || s != "" {
		t.Errorf("bad json payload: email=%q sub=%q", e, s)
	}
}

func TestPKCEChallenge(t *testing.T) {
	codes, err := generatePKCECodes()
	if err != nil {
		t.Fatalf("generatePKCECodes error: %v", err)
	}
	if len(codes.CodeVerifier) < 43 || len(codes.CodeVerifier) > 128 {
		t.Errorf("verifier length = %d, want RFC 7636 43..128", len(codes.CodeVerifier))
	}
	if codes.CodeVerifier == codes.CodeChallenge {
		t.Error("challenge must differ from verifier")
	}
}
