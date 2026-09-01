// Package iflow implements the iFlow AI OAuth2 authorization-code flow.
//
// iFlow differs from the other providers:
//   - No PKCE (plain authorization_code grant).
//   - Token exchange uses HTTP Basic auth with clientId:clientSecret (both are
//     public values embedded in 9router's open-source registry, not secrets).
//   - The authorization URL carries loginMethod=phone & type=phone.
//   - A mandatory post-exchange call to /api/oauth/getUserInfo returns the
//     provider-scoped apiKey (stored in ProviderSpecific["api_key"]) plus the
//     account email/phone. The executor uses that apiKey for request signing
//     and bearer auth.
package iflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

const (
	// ClientID is the public iFlow OAuth client ID (from 9router's registry).
	ClientID = "10009311001"
	// ClientSecret is the public iFlow OAuth client secret (from 9router's
	// open-source registry — not a private credential).
	ClientSecret = "4Z3YjXycVsQvyGF1etiNlIBB4RsqSDtW"
	// AuthorizeURL is iFlow's OAuth authorization endpoint.
	AuthorizeURL = "https://iflow.cn/oauth"
	// TokenURL is iFlow's OAuth token endpoint.
	TokenURL = "https://iflow.cn/oauth/token"
	// UserInfoURL is iFlow's user-info endpoint that returns the apiKey.
	UserInfoURL = "https://iflow.cn/api/oauth/getUserInfo"
	// CallbackPath is the local callback path.
	CallbackPath = "/callback"

	httpClientTimeout = 30 * time.Second
)

// OAuthService handles the iFlow authorization-code flow.
type OAuthService struct {
	httpClient *http.Client
	mu         sync.Mutex
	statePorts map[string]int

	// testTokenURL / testUserInfoURL override endpoint resolution in tests.
	testTokenURL    string
	testUserInfoURL string
}

// NewOAuthService creates a new iFlow OAuth service.
func NewOAuthService(httpClient *http.Client) *OAuthService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: httpClientTimeout}
	}
	return &OAuthService{
		httpClient: httpClient,
		statePorts: make(map[string]int),
	}
}

func (s *OAuthService) tokenURL() string {
	if s.testTokenURL != "" {
		return s.testTokenURL
	}
	return TokenURL
}

func (s *OAuthService) userInfoURL() string {
	if s.testUserInfoURL != "" {
		return s.testUserInfoURL
	}
	return UserInfoURL
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

// GenerateAuthURL creates the iFlow authorization URL. The flow uses the
// phone login method and no PKCE, matching 9router's iflow.js.
func (s *OAuthService) GenerateAuthURL(_ context.Context, state string) (string, error) {
	stateParam := stateParam(state)
	port := portFromState(state, 1455)

	s.mu.Lock()
	s.statePorts[stateParam] = port
	s.mu.Unlock()

	params := url.Values{
		"loginMethod": {"phone"},
		"type":        {"phone"},
		"redirect":    {redirectURI(port)},
		"state":       {stateParam},
		"client_id":   {ClientID},
	}

	return fmt.Sprintf("%s?%s", AuthorizeURL, params.Encode()), nil
}

// ExchangeCode exchanges an authorization code for tokens and fetches the
// mandatory user info (apiKey + email/phone).
func (s *OAuthService) ExchangeCode(ctx context.Context, code string) (*auth.Credentials, error) {
	return s.exchangeCode(ctx, code, redirectURI(portFromState("", 1455)))
}

func (s *OAuthService) exchangeCode(ctx context.Context, code, redirectURI string) (*auth.Credentials, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {ClientID},
		"client_secret": {ClientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL(), strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("iflow oauth: create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	basicAuth := base64.StdEncoding.EncodeToString([]byte(ClientID + ":" + ClientSecret))
	req.Header.Set("Authorization", "Basic "+basicAuth)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iflow oauth: token request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("iflow oauth: read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iflow oauth: token exchange failed %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("iflow oauth: parse token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("iflow oauth: token response missing access_token")
	}

	// The user-info call is mandatory: it returns the apiKey used for request
	// signing and the account email/phone. Fail hard on missing apiKey/email,
	// matching 9router's postExchange validation.
	psd, err := s.fetchUserInfo(ctx, tokenResp.AccessToken)
	if err != nil {
		return nil, err
	}

	creds := &auth.Credentials{
		AccessToken:      tokenResp.AccessToken,
		RefreshToken:     tokenResp.RefreshToken,
		ProviderSpecific: psd,
	}
	if tokenResp.ExpiresIn > 0 {
		creds.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	if email := psd["email"]; email != "" {
		creds.Email = email
	}
	if id := psd["user_id"]; id != "" {
		creds.AccountID = id
	}

	return creds, nil
}

// fetchUserInfo calls iFlow's /api/oauth/getUserInfo and validates the response.
func (s *OAuthService) fetchUserInfo(ctx context.Context, accessToken string) (map[string]string, error) {
	u, err := url.Parse(s.userInfoURL())
	if err != nil {
		return nil, fmt.Errorf("iflow oauth: parse userinfo URL: %w", err)
	}
	q := u.Query()
	q.Set("accessToken", accessToken)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("iflow oauth: create userinfo request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iflow oauth: userinfo request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("iflow oauth: read userinfo response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iflow oauth: userinfo failed %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var userInfo struct {
		Success bool `json:"success"`
		Data    struct {
			APIKey   string `json:"apiKey"`
			Email    string `json:"email"`
			Phone    string `json:"phone"`
			Nickname string `json:"nickname"`
			Name     string `json:"name"`
			UserID   string `json:"userId"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return nil, fmt.Errorf("iflow oauth: parse userinfo response: %w", err)
	}
	if !userInfo.Success {
		msg := userInfo.Message
		if msg == "" {
			msg = "Unknown error"
		}
		return nil, fmt.Errorf("iflow oauth: user info request failed: %s", msg)
	}

	apiKey := strings.TrimSpace(userInfo.Data.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("iflow oauth: empty API key returned from iFlow")
	}
	email := strings.TrimSpace(userInfo.Data.Email)
	phone := strings.TrimSpace(userInfo.Data.Phone)
	account := email
	if account == "" {
		account = phone
	}
	if account == "" {
		return nil, fmt.Errorf("iflow oauth: missing account email/phone in user info")
	}

	psd := map[string]string{
		"api_key": apiKey,
		"email":   account,
	}
	if phone != "" {
		psd["phone"] = phone
	}
	if nickname := strings.TrimSpace(userInfo.Data.Nickname); nickname != "" {
		psd["nickname"] = nickname
	}
	if name := strings.TrimSpace(userInfo.Data.Name); name != "" {
		psd["name"] = name
	}
	if userID := strings.TrimSpace(userInfo.Data.UserID); userID != "" {
		psd["user_id"] = userID
	}
	return psd, nil
}

// RefreshToken refreshes an expired iFlow access token.
func (s *OAuthService) RefreshToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	if creds.RefreshToken == "" {
		return nil, fmt.Errorf("iflow oauth: no refresh token available")
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {creds.RefreshToken},
		"client_id":     {ClientID},
		"client_secret": {ClientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL(), strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("iflow oauth: create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	basicAuth := base64.StdEncoding.EncodeToString([]byte(ClientID + ":" + ClientSecret))
	req.Header.Set("Authorization", "Basic "+basicAuth)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("iflow oauth: refresh request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("iflow oauth: read refresh response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("iflow oauth: token refresh failed %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("iflow oauth: parse refresh response: %w", err)
	}

	newCreds := *creds
	newCreds.AccessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		newCreds.RefreshToken = tokenResp.RefreshToken
	}
	if tokenResp.ExpiresIn > 0 {
		newCreds.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	return &newCreds, nil
}

// StartLocalServer starts a local HTTP server to receive the OAuth callback.
func (s *OAuthService) StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error) {
	stateParam := stateParam(state)
	resultChan := make(chan *auth.Credentials, 1)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, nil, fmt.Errorf("iflow oauth: listen: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	s.mu.Lock()
	s.statePorts[stateParam] = port
	s.mu.Unlock()

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

		creds, err := s.exchangeCode(r.Context(), code, redirectURI(port))
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
