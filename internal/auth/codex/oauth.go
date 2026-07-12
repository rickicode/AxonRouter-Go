package codex

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
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

// OAuth configuration constants for OpenAI Codex
const (
	AuthURL     = "https://auth.openai.com/oauth/authorize"
	TokenURL    = "https://auth.openai.com/oauth/token"
	ClientID    = "app_EMoamEEZ73f0CkXaXp7hrann"
	RedirectURI = "http://localhost:%d/auth/callback"
)

// OAuthService handles Codex OAuth2 PKCE flow.
type OAuthService struct {
	httpClient *http.Client
	mu         sync.Mutex
	pkce       map[string]string
	tokenURL   string
	deviceURL  string
}

// NewOAuthService creates a new Codex OAuth service.
func NewOAuthService(httpClient *http.Client) *OAuthService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &OAuthService{
		httpClient: httpClient,
		pkce:       make(map[string]string),
		tokenURL:   TokenURL,
		deviceURL:  DeviceCodeURL,
	}
}

// refreshError surfaces the HTTP status code from a failed token refresh so
// callers can decide whether to retry.
type refreshError struct {
	StatusCode int
	Body       string
}

func (e *refreshError) Error() string {
	return fmt.Sprintf("token refresh failed %d: %s", e.StatusCode, e.Body)
}

// IsAuthFailure reports whether the error indicates invalid or revoked credentials.
func isAuthFailure(err error) bool {
	if e, ok := err.(*refreshError); ok {
		return e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden
	}
	return false
}

// RefreshTokenWithRetry refreshes the access token, retrying on transient
// failures (5xx, network errors) with exponential backoff (100ms, 200ms).
// It fails fast on 401/403 because retrying will not help.
func (s *OAuthService) RefreshTokenWithRetry(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt*100) * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		newCreds, err := s.RefreshToken(ctx, creds)
		if err == nil {
			return newCreds, nil
		}
		lastErr = err
		if isAuthFailure(err) {
			break
		}
	}
	return nil, lastErr
}

// GenerateAuthURL creates the OAuth authorization URL with PKCE.
func (s *OAuthService) GenerateAuthURL(ctx context.Context, state string) (string, error) {
	// Extract port from state (format: "state:port")
	parts := strings.SplitN(state, ":", 2)
	stateParam := parts[0]
	port := 1455
	if len(parts) > 1 {
		fmt.Sscanf(parts[1], "%d", &port)
	}

	// Generate PKCE codes
	pkce, err := GeneratePKCECodes()
	if err != nil {
		return "", fmt.Errorf("generate PKCE: %w", err)
	}

	s.mu.Lock()
	s.pkce[stateParam] = pkce.CodeVerifier
	s.mu.Unlock()
	redirectURI := fmt.Sprintf(RedirectURI, port)

	params := url.Values{
		"client_id":                  {ClientID},
		"response_type":              {"code"},
		"redirect_uri":               {redirectURI},
		"scope":                      {"openid profile email offline_access"},
		"state":                      {stateParam},
		"code_challenge":             {pkce.CodeChallenge},
		"code_challenge_method":      {"S256"},
		"prompt":                     {"login"},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"originator":                 {"codex_cli_rs"},
	}

	return fmt.Sprintf("%s?%s", AuthURL, params.Encode()), nil
}

// ExchangeCode exchanges an authorization code for tokens.
func (s *OAuthService) ExchangeCode(ctx context.Context, code string) (*auth.Credentials, error) {
	return s.exchangeCode(ctx, code, fmt.Sprintf(RedirectURI, 1455), "")
}

func (s *OAuthService) exchangeCode(ctx context.Context, code, redirectURI, codeVerifier string) (*auth.Credentials, error) {
	data := url.Values{
		"grant_type":   {"authorization_code"},
		"client_id":    {ClientID},
		"code":         {code},
		"redirect_uri": {redirectURI},
	}
	if codeVerifier != "" {
		data.Set("code_verifier", codeVerifier)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", s.tokenURL, strings.NewReader(data.Encode()))
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
		Scope        string `json:"scope"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	// Extract account info from ID token
	accountID, email := extractTokenClaims(tokenResp.IDToken)

	return &auth.Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		AccountID:    accountID,
		Email:        email,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}, nil
}

// RefreshToken refreshes an expired access token.
func (s *OAuthService) RefreshToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	if creds.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {ClientID},
		"refresh_token": {creds.RefreshToken},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.tokenURL, strings.NewReader(data.Encode()))
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
		return nil, &refreshError{StatusCode: resp.StatusCode, Body: string(body)}
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
		accountID, email := extractTokenClaims(tokenResp.IDToken)
		if accountID != "" {
			newCreds.AccountID = accountID
		}
		if email != "" {
			newCreds.Email = email
		}
	}
	newCreds.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	return &newCreds, nil
}

// StartLocalServer starts a local HTTP server to receive the OAuth callback.
func (s *OAuthService) StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error) {
	parts := strings.SplitN(state, ":", 2)
	stateParam := parts[0]

	resultChan := make(chan *auth.Credentials, 1)
	var port int

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

		s.mu.Lock()
		codeVerifier := s.pkce[stateParam]
		delete(s.pkce, stateParam)
		s.mu.Unlock()
		if codeVerifier == "" {
			http.Error(w, "PKCE verifier missing", http.StatusBadRequest)
			return
		}

		creds, err := s.exchangeCode(r.Context(), code, fmt.Sprintf(RedirectURI, port), codeVerifier)
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

	// Codex OAuth app is registered with http://localhost:1455/auth/callback.
	// Must listen on the fixed port so the redirect_uri matches.
	listener, err := net.Listen("tcp", "127.0.0.1:1455")
	if err != nil {
		return 0, nil, fmt.Errorf("listen on port 1455: %w (is another OAuth flow already running?)", err)
	}
	port = 1455

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

// GeneratePKCECodes generates PKCE codes for OAuth flow.
func GeneratePKCECodes() (*PKCECodes, error) {
	bytes := make([]byte, 96)
	if _, err := rand.Read(bytes); err != nil {
		return nil, fmt.Errorf("generate random: %w", err)
	}

	verifier := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(bytes)
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(hash[:])

	return &PKCECodes{
		CodeVerifier:  verifier,
		CodeChallenge: challenge,
	}, nil
}

// extractTokenClaims extracts account_id and email from a JWT ID token.
// ponytail: minimal JWT parsing, just decode the payload section.
func extractTokenClaims(idToken string) (accountID, email string) {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return "", ""
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ""
	}

	var claims struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", ""
	}

	return claims.Sub, claims.Email
}
