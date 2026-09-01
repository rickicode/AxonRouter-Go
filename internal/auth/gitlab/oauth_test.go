package gitlab

import (
	"context"
	"crypto/sha256"
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
	t.Setenv("GITLAB_OAUTH_CLIENT_ID", "gitlab-client-id")
	t.Setenv("GITLAB_OAUTH_BASE_URL", "https://gitlab.example.com")

	svc := NewOAuthService(http.DefaultClient)
	authURL, err := svc.GenerateAuthURL(context.Background(), "randomstate:1234")
	if err != nil {
		t.Fatalf("GenerateAuthURL error: %v", err)
	}

	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("parse auth URL: %v", err)
	}
	if u.Scheme != "https" || u.Host != "gitlab.example.com" || u.Path != "/oauth/authorize" {
		t.Errorf("unexpected authorize URL: %s", authURL)
	}
	q := u.Query()
	if q.Get("client_id") != "gitlab-client-id" {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "http://127.0.0.1:1234/callback" {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q", q.Get("response_type"))
	}
	if q.Get("scope") != Scope {
		t.Errorf("scope = %q, want %q", q.Get("scope"), Scope)
	}
	if q.Get("state") != "randomstate" {
		t.Errorf("state = %q, want randomstate (port stripped)", q.Get("state"))
	}
	if q.Get("code_challenge") == "" {
		t.Error("code_challenge missing")
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
}

func TestGenerateAuthURL_MissingClientID(t *testing.T) {
	t.Setenv("GITLAB_OAUTH_CLIENT_ID", "")
	svc := NewOAuthService(http.DefaultClient)
	if _, err := svc.GenerateAuthURL(context.Background(), "state:1"); err == nil {
		t.Fatal("expected error when GITLAB_OAUTH_CLIENT_ID is not set")
	}
}

func TestExchangeCode(t *testing.T) {
	var gotForm url.Values
	var gotAuth string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			gotAuth = r.Header.Get("Authorization")
			body, _ := io.ReadAll(r.Body)
			gotForm, _ = url.ParseQuery(string(body))
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "gl-access-123",
				"refresh_token": "gl-refresh-456",
				"expires_in":    7200,
				"token_type":    "Bearer",
				"scope":         "api read_user",
			})
		case "/api/v4/user":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id":          42,
				"username":    "johndoe",
				"name":        "John Doe",
				"email":       "john@example.com",
				"public_email": "",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	svc := NewOAuthService(http.DefaultClient)
	svc.testAuthURL = ts.URL + "/oauth/authorize"
	svc.testTokenURL = ts.URL + "/oauth/token"
	svc.testUserInfoURL = ts.URL + "/api/v4/user"
	t.Setenv("GITLAB_OAUTH_CLIENT_ID", "client-id")
	t.Setenv("GITLAB_OAUTH_CLIENT_SECRET", "client-secret")
	t.Setenv("GITLAB_OAUTH_BASE_URL", "https://gitlab.example.com")

	creds, err := svc.ExchangeCode(context.Background(), "auth-code-xyz")
	if err != nil {
		t.Fatalf("ExchangeCode error: %v", err)
	}
	if creds.AccessToken != "gl-access-123" {
		t.Errorf("AccessToken = %q", creds.AccessToken)
	}
	if creds.RefreshToken != "gl-refresh-456" {
		t.Errorf("RefreshToken = %q", creds.RefreshToken)
	}
	if !creds.ExpiresAt.After(time.Now()) {
		t.Error("ExpiresAt not set")
	}
	if creds.Email != "john@example.com" {
		t.Errorf("Email = %q", creds.Email)
	}
	if creds.AccountID != "42" {
		t.Errorf("AccountID = %q", creds.AccountID)
	}
	if creds.ProviderSpecific["username"] != "johndoe" {
		t.Errorf("username = %q", creds.ProviderSpecific["username"])
	}
	if creds.ProviderSpecific["name"] != "John Doe" {
		t.Errorf("name = %q", creds.ProviderSpecific["name"])
	}
	if creds.ProviderSpecific["email"] != "john@example.com" {
		t.Errorf("psd email = %q", creds.ProviderSpecific["email"])
	}
	if creds.ProviderSpecific["user_id"] != "42" {
		t.Errorf("user_id = %q", creds.ProviderSpecific["user_id"])
	}
	if creds.ProviderSpecific["baseUrl"] != "https://gitlab.example.com" {
		t.Errorf("baseUrl = %q", creds.ProviderSpecific["baseUrl"])
	}
	if creds.ProviderSpecific["clientId"] != "client-id" {
		t.Errorf("clientId = %q", creds.ProviderSpecific["clientId"])
	}
	if creds.ProviderSpecific["authKind"] != "oauth" {
		t.Errorf("authKind = %q", creds.ProviderSpecific["authKind"])
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
	if gotForm.Get("code_verifier") != "" {
		t.Errorf("standalone ExchangeCode should not send code_verifier, got %q", gotForm.Get("code_verifier"))
	}
	if gotAuth != "" {
		t.Errorf("gitlab uses body credentials, got Authorization %q", gotAuth)
	}
}

func TestRefreshToken(t *testing.T) {
	var gotForm url.Values

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	t.Setenv("GITLAB_OAUTH_CLIENT_ID", "client-id")
	t.Setenv("GITLAB_OAUTH_CLIENT_SECRET", "client-secret")

	svc := NewOAuthService(http.DefaultClient)
	svc.testTokenURL = ts.URL + "/oauth/token"
	creds, err := svc.RefreshToken(context.Background(), &auth.Credentials{
		RefreshToken: "old-refresh",
		ProviderSpecific: map[string]string{
			"token_endpoint": ts.URL + "/oauth/token",
			"clientId":       "client-id",
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
	if gotForm.Get("grant_type") != "refresh_token" {
		t.Errorf("grant_type = %q", gotForm.Get("grant_type"))
	}
	if gotForm.Get("refresh_token") != "old-refresh" {
		t.Errorf("refresh_token = %q", gotForm.Get("refresh_token"))
	}
	if gotForm.Get("client_secret") != "client-secret" {
		t.Errorf("client_secret = %q", gotForm.Get("client_secret"))
	}
}

func TestRefreshToken_NoSecret(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if form.Get("client_secret") != "" {
			t.Errorf("client_secret sent but not configured")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    7200,
		})
	}))
	defer ts.Close()

	t.Setenv("GITLAB_OAUTH_CLIENT_ID", "client-id")
	t.Setenv("GITLAB_OAUTH_CLIENT_SECRET", "")

	svc := NewOAuthService(http.DefaultClient)
	svc.testTokenURL = ts.URL + "/oauth/token"
	creds, err := svc.RefreshToken(context.Background(), &auth.Credentials{
		RefreshToken: "old-refresh",
		ProviderSpecific: map[string]string{
			"token_endpoint": ts.URL + "/oauth/token",
			"clientId":       "client-id",
		},
	})
	if err != nil {
		t.Fatalf("RefreshToken error: %v", err)
	}
	if creds.AccessToken != "new-access" {
		t.Errorf("AccessToken = %q", creds.AccessToken)
	}
}

func TestStartLocalServer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
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
				"expires_in":    3600,
			})
		case "/api/v4/user":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"id":       42,
				"username": "localuser",
				"name":     "Local User",
				"email":    "local@example.com",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	t.Setenv("GITLAB_OAUTH_CLIENT_ID", "client-id")
	svc := NewOAuthService(http.DefaultClient)
	svc.testAuthURL = ts.URL + "/oauth/authorize"
	svc.testTokenURL = ts.URL + "/oauth/token"
	svc.testUserInfoURL = ts.URL + "/api/v4/user"

	// GenerateAuthURL stores the PKCE verifier for "srv-state".
	if _, err := svc.GenerateAuthURL(context.Background(), "srv-state:0"); err != nil {
		t.Fatalf("GenerateAuthURL error: %v", err)
	}

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

func TestStartLocalServer_StateMismatch(t *testing.T) {
	t.Setenv("GITLAB_OAUTH_CLIENT_ID", "client-id")
	svc := NewOAuthService(http.DefaultClient)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	port, resultChan, err := svc.StartLocalServer(ctx, "expected-state")
	if err != nil {
		t.Fatalf("StartLocalServer error: %v", err)
	}

	callbackURL := "http://127.0.0.1:" + strconv.Itoa(port) + "/callback?code=code&state=wrong-state"
	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("callback request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("callback status = %d, want 400", resp.StatusCode)
	}

	select {
	case creds := <-resultChan:
		t.Fatalf("unexpected credentials delivered: %+v", creds)
	case <-time.After(200 * time.Millisecond):
		// Expected: no credentials.
	}
}

func TestPKCEChallenge(t *testing.T) {
	codes, err := generatePKCECodes()
	if err != nil {
		t.Fatalf("generatePKCECodes error: %v", err)
	}
	if codes.CodeVerifier == "" || codes.CodeChallenge == "" {
		t.Fatal("empty PKCE codes")
	}
	if len(codes.CodeVerifier) < 43 || len(codes.CodeVerifier) > 128 {
		t.Errorf("verifier length = %d, want RFC 7636 43..128", len(codes.CodeVerifier))
	}
	// S256: challenge must equal base64url(sha256(verifier)) without padding.
	hash := sha256.Sum256([]byte(codes.CodeVerifier))
	expected := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(hash[:])
	if codes.CodeChallenge != expected {
		t.Error("code challenge is not the S256 hash of the verifier")
	}
}

func TestPortFromState(t *testing.T) {
	if got := portFromState("state:4567", 1455); got != 4567 {
		t.Errorf("portFromState(state:4567) = %d", got)
	}
	if got := portFromState("state", 1455); got != 1455 {
		t.Errorf("portFromState(state) = %d, want default", got)
	}
	if got := portFromState("state:notaport", 1455); got != 1455 {
		t.Errorf("portFromState(state:notaport) = %d, want default", got)
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

func TestGenerateAuthURL_UsesConfiguredBaseURL(t *testing.T) {
	t.Setenv("GITLAB_OAUTH_CLIENT_ID", "client-id")
	t.Setenv("GITLAB_OAUTH_BASE_URL", "https://gitlab.company.internal")

	svc := NewOAuthService(http.DefaultClient)
	authURL, err := svc.GenerateAuthURL(context.Background(), "state:1")
	if err != nil {
		t.Fatalf("GenerateAuthURL error: %v", err)
	}
	if !strings.HasPrefix(authURL, "https://gitlab.company.internal/oauth/authorize?") {
		t.Errorf("auth URL = %q", authURL)
	}
}
