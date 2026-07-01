package kiro

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

// OAuth configuration for Kiro (AWS)
const (
	AuthURL    = "https://kiro.auth.us-east-1.amazoncognito.com/oauth2/authorize"
	TokenURL   = "https://kiro.auth.us-east-1.amazoncognito.com/oauth2/token"
	ClientID   = "4l5l3n4v1njkqjqjqjqjqj"
	RedirectURI = "http://localhost:%d/auth/callback"
	Scopes     = "openid email profile"
)

// OAuthService handles AWS OAuth flow for Kiro.
type OAuthService struct {
	httpClient *http.Client
}

// NewOAuthService creates a new Kiro OAuth service.
func NewOAuthService(httpClient *http.Client) *OAuthService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &OAuthService{httpClient: httpClient}
}

// GenerateAuthURL creates the AWS OAuth authorization URL.
func (s *OAuthService) GenerateAuthURL(ctx context.Context, state string) (string, error) {
	parts := strings.SplitN(state, ":", 2)
	stateParam := parts[0]
	port := 1457
	if len(parts) > 1 {
		fmt.Sscanf(parts[1], "%d", &port)
	}

	redirectURI := fmt.Sprintf(RedirectURI, port)

	params := url.Values{
		"client_id":     {ClientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {Scopes},
		"state":         {stateParam},
	}

	return fmt.Sprintf("%s?%s", AuthURL, params.Encode()), nil
}

// ExchangeCode exchanges an authorization code for tokens.
func (s *OAuthService) ExchangeCode(ctx context.Context, code string) (*auth.Credentials, error) {
	data := url.Values{
		"grant_type":   {"authorization_code"},
		"client_id":    {ClientID},
		"code":         {code},
		"redirect_uri": {fmt.Sprintf(RedirectURI, 1457)},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	return &auth.Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}, nil
}

// RefreshToken refreshes an expired Kiro access token.
func (s *OAuthService) RefreshToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	if creds.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {ClientID},
		"refresh_token": {creds.RefreshToken},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	newCreds := *creds
	newCreds.AccessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		newCreds.RefreshToken = tokenResp.RefreshToken
	}
	if tokenResp.IDToken != "" {
		newCreds.IDToken = tokenResp.IDToken
	}
	newCreds.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	return &newCreds, nil
}

// StartLocalServer starts a local HTTP server for the OAuth callback.
func (s *OAuthService) StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error) {
	parts := strings.SplitN(state, ":", 2)
	stateParam := parts[0]

	resultChan := make(chan *auth.Credentials, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
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

		creds, err := s.ExchangeCode(r.Context(), code)
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

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, nil, fmt.Errorf("listen: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

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
