// Package gitlab implements the GitLab Duo OAuth2 authorization-code + PKCE flow.
//
// GitLab requires an operator-supplied OAuth application (client ID/secret) and
// optionally a self-hosted base URL. Defaults target gitlab.com. All values can
// be overridden through environment variables so the flow works without any
// frontend configuration:
//
//	GITLAB_OAUTH_BASE_URL       (default https://gitlab.com)
//	GITLAB_OAUTH_CLIENT_ID      (required unless the frontend supplies meta)
//	GITLAB_OAUTH_CLIENT_SECRET  (optional; some GitLab instances require it)
package gitlab

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

const (
	// DefaultBaseURL is the GitLab.com host.
	DefaultBaseURL = "https://gitlab.com"
	// Scope is the OAuth scope set required for GitLab Duo access.
	Scope = "api read_user"
	// AuthorizeURLPath is the OAuth authorization endpoint path.
	AuthorizeURLPath = "/oauth/authorize"
	// TokenURLPath is the OAuth token endpoint path.
	TokenURLPath = "/oauth/token"
	// UserInfoURLPath is the GitLab user endpoint path.
	UserInfoURLPath = "/api/v4/user"
	// CallbackPath is the local callback path.
	CallbackPath = "/callback"

	httpClientTimeout = 30 * time.Second
)

// envOrDefault returns the environment variable value, or def when unset/blank.
func envOrDefault(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// OAuthService handles the GitLab authorization-code + PKCE flow.
type OAuthService struct {
	httpClient *http.Client
	mu         sync.Mutex
	pkce       map[string]string

	// testAuthURL / testTokenURL / testUserInfoURL override endpoint resolution
	// in tests.
	testAuthURL    string
	testTokenURL   string
	testUserInfoURL string
}

// NewOAuthService creates a new GitLab OAuth service.
func NewOAuthService(httpClient *http.Client) *OAuthService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: httpClientTimeout}
	}
	return &OAuthService{
		httpClient: httpClient,
		pkce:       make(map[string]string),
	}
}

// baseURL returns the configured GitLab base URL.
func (s *OAuthService) baseURL() string { return envOrDefault("GITLAB_OAUTH_BASE_URL", DefaultBaseURL) }

// clientID returns the configured GitLab OAuth client ID.
func (s *OAuthService) clientID() string { return envOrDefault("GITLAB_OAUTH_CLIENT_ID", "") }

// clientSecret returns the configured GitLab OAuth client secret.
func (s *OAuthService) clientSecret() string {
	return envOrDefault("GITLAB_OAUTH_CLIENT_SECRET", "")
}

func (s *OAuthService) authorizeURL() string {
	if s.testAuthURL != "" {
		return s.testAuthURL
	}
	return strings.TrimRight(s.baseURL(), "/") + AuthorizeURLPath
}

func (s *OAuthService) tokenURL() string {
	if s.testTokenURL != "" {
		return s.testTokenURL
	}
	return strings.TrimRight(s.baseURL(), "/") + TokenURLPath
}

func (s *OAuthService) userInfoURL() string {
	if s.testUserInfoURL != "" {
		return s.testUserInfoURL
	}
	return strings.TrimRight(s.baseURL(), "/") + UserInfoURLPath
}

func stateParam(state string) string {
	parts := strings.SplitN(state, ":", 2)
	return parts[0]
}

func portFromState(state string, def int) int {
	parts := strings.SplitN(state, ":", 2)
	if len(parts) < 2 {
		return def
	}
	var port int
	if _, err := fmt.Sscanf(parts[1], "%d", &port); err == nil && port > 0 {
		return port
	}
	return def
}

func redirectURI(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", port, CallbackPath)
}

// GenerateAuthURL creates the GitLab authorization URL with PKCE.
func (s *OAuthService) GenerateAuthURL(_ context.Context, state string) (string, error) {
	clientID := s.clientID()
	if clientID == "" {
		return "", fmt.Errorf("gitlab oauth: GITLAB_OAUTH_CLIENT_ID is not set; create an OAuth application in GitLab and configure the env var")
	}

	stateParam := stateParam(state)
	port := portFromState(state, 1455)

	pkce, err := generatePKCECodes()
	if err != nil {
		return "", fmt.Errorf("gitlab oauth: generate PKCE: %w", err)
	}
	s.mu.Lock()
	s.pkce[stateParam] = pkce.CodeVerifier
	s.mu.Unlock()

	params := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI(port)},
		"response_type":         {"code"},
		"scope":                 {Scope},
		"state":                 {stateParam},
		"code_challenge":        {pkce.CodeChallenge},
		"code_challenge_method": {"S256"},
	}

	return fmt.Sprintf("%s?%s", s.authorizeURL(), params.Encode()), nil
}

// ExchangeCode exchanges an authorization code for tokens.
func (s *OAuthService) ExchangeCode(ctx context.Context, code string) (*auth.Credentials, error) {
	return s.exchangeCode(ctx, code, redirectURI(portFromState("", 1455)), "")
}

func (s *OAuthService) exchangeCode(ctx context.Context, code, redirectURI, codeVerifier string) (*auth.Credentials, error) {
	clientID := s.clientID()
	if clientID == "" {
		return nil, fmt.Errorf("gitlab oauth: GITLAB_OAUTH_CLIENT_ID is not set")
	}

	data := url.Values{
		"grant_type":   {"authorization_code"},
		"client_id":    {clientID},
		"code":         {code},
		"redirect_uri": {redirectURI},
	}
	if secret := s.clientSecret(); secret != "" {
		data.Set("client_secret", secret)
	}
	if codeVerifier != "" {
		data.Set("code_verifier", codeVerifier)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL(), strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("gitlab oauth: create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab oauth: token request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gitlab oauth: read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab oauth: token exchange failed %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("gitlab oauth: parse token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("gitlab oauth: token response missing access_token")
	}

	creds := &auth.Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
	}
	if tokenResp.ExpiresIn > 0 {
		creds.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	// Fetch the GitLab user profile to build a stable account key and display
	// name. Non-fatal: on failure we still return the tokens.
	psd, err := s.fetchUserInfo(ctx, tokenResp.AccessToken)
	if err == nil && psd != nil {
		creds.ProviderSpecific = psd
		if email := psd["email"]; email != "" {
			creds.Email = email
		}
		if id := psd["user_id"]; id != "" {
			creds.AccountID = id
		}
	}
	if creds.ProviderSpecific == nil {
		creds.ProviderSpecific = map[string]string{}
	}
	// Persist the base URL + client ID so RefreshToken can rebuild endpoints
	// even when the operator changes env vars after the flow completed.
	creds.ProviderSpecific["baseUrl"] = s.baseURL()
	creds.ProviderSpecific["clientId"] = s.clientID()
	creds.ProviderSpecific["authKind"] = "oauth"

	return creds, nil
}

// fetchUserInfo fetches the GitLab /api/v4/user profile.
func (s *OAuthService) fetchUserInfo(ctx context.Context, accessToken string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.userInfoURL(), nil)
	if err != nil {
		return nil, fmt.Errorf("gitlab oauth: create userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab oauth: userinfo request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gitlab oauth: read userinfo response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab oauth: userinfo failed %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var user struct {
		ID          int64  `json:"id"`
		Username    string `json:"username"`
		Name        string `json:"name"`
		Email       string `json:"email"`
		PublicEmail string `json:"public_email"`
	}
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, fmt.Errorf("gitlab oauth: parse userinfo response: %w", err)
	}

	psd := map[string]string{}
	if user.ID != 0 {
		psd["user_id"] = fmt.Sprintf("%d", user.ID)
	}
	if user.Username != "" {
		psd["username"] = user.Username
	}
	if user.Name != "" {
		psd["name"] = user.Name
	}
	email := strings.TrimSpace(user.Email)
	if email == "" {
		email = strings.TrimSpace(user.PublicEmail)
	}
	if email != "" {
		psd["email"] = email
	}
	return psd, nil
}

// RefreshToken refreshes an expired GitLab access token.
func (s *OAuthService) RefreshToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	if creds.RefreshToken == "" {
		return nil, fmt.Errorf("gitlab oauth: no refresh token available")
	}

	// Endpoints are rebuilt from the PSD stored at connect time so a changed
	// env var does not invalidate refresh for self-hosted instances.
	tokenURL := s.tokenURL()
	clientID := s.clientID()
	if creds.ProviderSpecific != nil {
		if v := strings.TrimSpace(creds.ProviderSpecific["token_endpoint"]); v != "" {
			tokenURL = v
		}
		if v := strings.TrimSpace(creds.ProviderSpecific["clientId"]); v != "" {
			clientID = v
		}
	}
	if tokenURL == "" || clientID == "" {
		return nil, fmt.Errorf("gitlab oauth: token endpoint or client id unavailable")
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {creds.RefreshToken},
	}
	if secret := s.clientSecret(); secret != "" {
		data.Set("client_secret", secret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("gitlab oauth: create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab oauth: refresh request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gitlab oauth: read refresh response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gitlab oauth: token refresh failed %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("gitlab oauth: parse refresh response: %w", err)
	}

	newCreds := *creds
	newCreds.AccessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		newCreds.RefreshToken = tokenResp.RefreshToken
	}
	if tokenResp.ExpiresIn > 0 {
		newCreds.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	if newCreds.ProviderSpecific == nil {
		newCreds.ProviderSpecific = map[string]string{}
	}
	newCreds.ProviderSpecific["token_endpoint"] = tokenURL

	return &newCreds, nil
}

// StartLocalServer starts a local HTTP server to receive the OAuth callback.
func (s *OAuthService) StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error) {
	stateParam := stateParam(state)
	resultChan := make(chan *auth.Credentials, 1)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, nil, fmt.Errorf("gitlab oauth: listen: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc(CallbackPath, func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		returnedState := r.URL.Query().Get("state")
		if returnedState != stateParam {
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}
		if code == "" {
			http.Error(w, "No code received", http.StatusBadRequest)
			return
		}

		s.mu.Lock()
		codeVerifier := s.pkce[stateParam]
		delete(s.pkce, stateParam)
		s.mu.Unlock()
		if codeVerifier == "" {
			http.Error(w, "PKCE verifier missing", http.StatusBadRequest)
			return
		}

		creds, err := s.exchangeCode(r.Context(), code, redirectURI(port), codeVerifier)
		if err != nil {
			http.Error(w, fmt.Sprintf("Token exchange failed: %v", err), http.StatusInternalServerError)
			return
		}

		resultChan <- creds

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Auth Success</title>
<script>setTimeout(function(){window.close();},3000);</script></head>
<body><h1>Authentication successful!</h1><p>You can close this window.</p></body></html>`)
	})

	server := &http.Server{
		Handler: mux,
		BaseContext: func(l net.Listener) context.Context {
			return ctx
		},
	}
	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()
	go func() {
		if err := server.Serve(listener); err != http.ErrServerClosed {
			close(resultChan)
		}
	}()
	return port, resultChan, nil
}

// PKCECodes holds PKCE challenge/verifier pair.
type PKCECodes struct {
	CodeVerifier  string
	CodeChallenge string
}

// generatePKCECodes generates PKCE codes (RFC 7636, S256).
func generatePKCECodes() (*PKCECodes, error) {
	b := make([]byte, 96)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generate random: %w", err)
	}
	verifier := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b)
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(hash[:])
	return &PKCECodes{CodeVerifier: verifier, CodeChallenge: challenge}, nil
}
