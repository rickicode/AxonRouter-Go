package qoder

import (
	"context"
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

// OAuthService implements the OAuth2 authorization-code flow for Qoder.
type OAuthService struct {
	httpClient   *http.Client
	mu           sync.Mutex
	statePorts   map[string]int
	UsePhoneAuth bool
}

// NewOAuthService creates a new Qoder OAuth service.
func NewOAuthService(httpClient *http.Client) *OAuthService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &OAuthService{
		httpClient: httpClient,
		statePorts: make(map[string]int),
	}
}

func envOrDefault(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func (s *OAuthService) authorizeURL() string  { return envOrDefault("QODER_OAUTH_AUTHORIZE_URL") }
func (s *OAuthService) tokenURL() string      { return envOrDefault("QODER_OAUTH_TOKEN_URL") }
func (s *OAuthService) userInfoURL() string   { return envOrDefault("QODER_OAUTH_USERINFO_URL") }
func (s *OAuthService) clientID() string      { return envOrDefault("QODER_OAUTH_CLIENT_ID") }
func (s *OAuthService) clientSecret() string  { return envOrDefault("QODER_OAUTH_CLIENT_SECRET") }

// stateParts splits "state:port" into its components.
func stateParts(state string) (stateParam string, port int) {
	parts := strings.SplitN(state, ":", 2)
	stateParam = parts[0]
	port = 0
	if len(parts) > 1 {
		fmt.Sscanf(parts[1], "%d", &port)
	}
	return stateParam, port
}

// redirectURI returns the localhost callback URL for a given port.
func redirectURI(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/callback", port)
}

// GenerateAuthURL creates the Qoder authorization URL.
func (s *OAuthService) GenerateAuthURL(ctx context.Context, state string) (string, error) {
	stateParam, port := stateParts(state)
	if port <= 0 {
		port = 1455
	}

	s.mu.Lock()
	s.statePorts[stateParam] = port
	s.mu.Unlock()

	params := url.Values{
		"client_id":     {s.clientID()},
		"redirect_uri":  {redirectURI(port)},
		"state":         {stateParam},
		"response_type": {"code"},
	}
	if s.UsePhoneAuth {
		params.Set("loginMethod", "phone")
		params.Set("type", "phone")
	}
	return fmt.Sprintf("%s?%s", s.authorizeURL(), params.Encode()), nil
}

// ExchangeCode exchanges an authorization code for tokens and fetches user info.
func (s *OAuthService) ExchangeCode(ctx context.Context, code string) (*auth.Credentials, error) {
	port := 1455
	s.mu.Lock()
	for _, p := range s.statePorts {
		port = p
		break
	}
	s.mu.Unlock()
	return s.exchangeCode(ctx, code, redirectURI(port))
}

func (s *OAuthService) exchangeCode(ctx context.Context, code, redirectURI string) (*auth.Credentials, error) {
	tokenURL := s.tokenURL()
	clientID := s.clientID()
	clientSecret := s.clientSecret()

	data := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
		"client_id":    {clientID},
	}
	if clientSecret != "" {
		data.Set("client_secret", clientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if clientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(clientSecret))
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}

	creds := &auth.Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
	}
	if tokenResp.ExpiresIn > 0 {
		creds.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	psd, err := s.fetchUserInfo(ctx, tokenResp.AccessToken)
	if err == nil && psd != nil {
		creds.ProviderSpecific = psd
		if email := psd["email"]; email != "" && creds.Email == "" {
			creds.Email = email
		}
	}
	return creds, nil
}

func (s *OAuthService) fetchUserInfo(ctx context.Context, accessToken string) (map[string]string, error) {
	userInfoURL := s.userInfoURL()
	if userInfoURL == "" {
		return map[string]string{}, nil
	}
	u, err := url.Parse(userInfoURL)
	if err != nil {
		return nil, fmt.Errorf("parse userinfo URL: %w", err)
	}
	q := u.Query()
	q.Set("accessToken", accessToken)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create userinfo request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read userinfo response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo request failed %d: %s", resp.StatusCode, string(body))
	}

	var userInfo struct {
		Success bool `json:"success"`
		Data    struct {
			APIKey   string `json:"apiKey"`
			Email    string `json:"email"`
			Nickname string `json:"nickname"`
			Phone    string `json:"phone"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return nil, fmt.Errorf("parse userinfo response: %w", err)
	}

	psd := map[string]string{}
	if userInfo.Data.APIKey != "" {
		psd["api_key"] = userInfo.Data.APIKey
	}
	if userInfo.Data.Email != "" {
		psd["email"] = userInfo.Data.Email
	}
	if userInfo.Data.Nickname != "" {
		psd["nickname"] = userInfo.Data.Nickname
	}
	if userInfo.Data.Phone != "" {
		psd["phone"] = userInfo.Data.Phone
	}
	return psd, nil
}

// RefreshToken refreshes an expired access token.
func (s *OAuthService) RefreshToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	if creds.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}
	tokenURL := s.tokenURL()
	clientID := s.clientID()
	clientSecret := s.clientSecret()

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {creds.RefreshToken},
		"client_id":     {clientID},
	}
	if clientSecret != "" {
		data.Set("client_secret", clientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if clientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(clientSecret))
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read refresh response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse refresh response: %w", err)
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
	stateParam, _ := stateParts(state)
	resultChan := make(chan *auth.Credentials, 1)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, nil, fmt.Errorf("listen: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	s.mu.Lock()
	s.statePorts[stateParam] = port
	s.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
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
