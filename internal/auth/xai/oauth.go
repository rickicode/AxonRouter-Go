// Package xai implements the xAI (Grok) OAuth2 authorization-code + PKCE flow.
//
// The flow uses xAI's OIDC discovery document to resolve the authorization and
// token endpoints, with a static fallback matching 9router's discoverXaiEndpoints().
// The callback server listens on a fixed loopback port (56121) because the public
// xAI OAuth client is registered with http://127.0.0.1:56121/callback.
package xai

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	// DiscoveryURL is xAI's OIDC discovery endpoint.
	DiscoveryURL = "https://auth.x.ai/.well-known/openid-configuration"
	// Issuer is xAI's OAuth issuer.
	Issuer = "https://auth.x.ai"
	// ClientID is the public Grok CLI OAuth client ID.
	ClientID = "b1a00492-073a-47ea-816f-4c329264a828"
	// Scope is the OAuth scope set required for xAI API access.
	Scope = "openid profile email offline_access grok-cli:access api:access"
	// LoopbackPort is the fixed loopback port the OAuth callback listens on.
	LoopbackPort = 56121
	// CallbackPath is the local callback path.
	CallbackPath = "/callback"
	// RedirectURI is the registered redirect URI.
	RedirectURI = "http://127.0.0.1:56121/callback"
	// APIBase is the xAI API base URL used for model requests.
	APIBase = "https://api.x.ai/v1"

	// Static fallback endpoints (9router xai.js fallback).
	fallbackAuthEndpoint  = "https://auth.x.ai/oauth2/authorize"
	fallbackTokenEndpoint = "https://auth.x.ai/oauth2/token"

	httpClientTimeout = 30 * time.Second
)

// Discovery holds OAuth endpoints resolved from xAI OIDC discovery.
type Discovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	Issuer                string `json:"issuer"`
}

// OAuthService handles the xAI authorization-code + PKCE flow.
type OAuthService struct {
	httpClient   *http.Client
	discoveryURL string
	mu           sync.Mutex
	pkce         map[string]string

	// testDiscoveryResponse, when set, is returned by Discover so tests can
	// avoid calling the real xAI OIDC discovery endpoint.
	testDiscoveryResponse *Discovery
}

// NewOAuthService creates a new xAI OAuth service.
func NewOAuthService(httpClient *http.Client) *OAuthService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: httpClientTimeout}
	}
	return &OAuthService{
		httpClient:   httpClient,
		discoveryURL: DiscoveryURL,
		pkce:         make(map[string]string),
	}
}

// Discover resolves xAI OAuth endpoints through OIDC discovery. On any failure
// it falls back to the static endpoints so the flow still works when xAI's
// discovery document is unavailable.
func (s *OAuthService) Discover(ctx context.Context) *Discovery {
	if s.testDiscoveryResponse != nil {
		return s.testDiscoveryResponse
	}
	if d, err := s.discover(ctx); err == nil {
		return d
	}
	return &Discovery{
		AuthorizationEndpoint: fallbackAuthEndpoint,
		TokenEndpoint:         fallbackTokenEndpoint,
		Issuer:                Issuer,
	}
}

func (s *OAuthService) discover(ctx context.Context) (*Discovery, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("xai discovery: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xai discovery: request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xai discovery: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("xai discovery failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var d Discovery
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("xai discovery: parse response: %w", err)
	}
	if strings.TrimSpace(d.AuthorizationEndpoint) == "" || strings.TrimSpace(d.TokenEndpoint) == "" {
		return nil, fmt.Errorf("xai discovery: missing authorization or token endpoint")
	}
	return &d, nil
}

func stateParam(state string) string {
	parts := strings.SplitN(state, ":", 2)
	return parts[0]
}

// GenerateAuthURL creates the xAI authorization URL with PKCE and a nonce.
func (s *OAuthService) GenerateAuthURL(ctx context.Context, state string) (string, error) {
	stateParam := stateParam(state)
	discovery := s.Discover(ctx)

	pkce, err := generatePKCECodes()
	if err != nil {
		return "", fmt.Errorf("xai oauth: generate PKCE: %w", err)
	}
	s.mu.Lock()
	s.pkce[stateParam] = pkce.CodeVerifier
	s.mu.Unlock()

	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", fmt.Errorf("xai oauth: generate nonce: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)

	params := url.Values{
		"client_id":             {ClientID},
		"redirect_uri":          {RedirectURI},
		"response_type":         {"code"},
		"scope":                 {Scope},
		"state":                 {stateParam},
		"nonce":                 {nonce},
		"plan":                  {"generic"},
		"referrer":              {"cli-proxy-api"},
		"code_challenge":        {pkce.CodeChallenge},
		"code_challenge_method": {"S256"},
	}

	return fmt.Sprintf("%s?%s", discovery.AuthorizationEndpoint, params.Encode()), nil
}

// ExchangeCode exchanges an authorization code for tokens.
func (s *OAuthService) ExchangeCode(ctx context.Context, code string) (*auth.Credentials, error) {
	return s.exchangeCode(ctx, code, RedirectURI, "")
}

func (s *OAuthService) exchangeCode(ctx context.Context, code, redirectURI, codeVerifier string) (*auth.Credentials, error) {
	discovery := s.Discover(ctx)
	data := url.Values{
		"grant_type":   {"authorization_code"},
		"client_id":    {ClientID},
		"code":         {code},
		"redirect_uri": {redirectURI},
	}
	if codeVerifier != "" {
		data.Set("code_verifier", codeVerifier)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, discovery.TokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("xai oauth: create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xai oauth: token request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xai oauth: read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("xai oauth: token exchange failed %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
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
		return nil, fmt.Errorf("xai oauth: parse token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("xai oauth: token response missing access_token")
	}

	email, sub := extractTokenClaims(tokenResp.IDToken)

	creds := &auth.Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		Email:        email,
		AccountID:    sub,
	}
	if tokenResp.ExpiresIn > 0 {
		creds.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	psd := map[string]string{}
	if tokenResp.IDToken != "" {
		psd["idToken"] = tokenResp.IDToken
	}
	if email != "" {
		psd["email"] = email
	}
	if sub != "" {
		psd["sub"] = sub
	}
	psd["token_endpoint"] = discovery.TokenEndpoint
	creds.ProviderSpecific = psd

	return creds, nil
}

// RefreshToken refreshes an expired xAI access token.
func (s *OAuthService) RefreshToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	if creds.RefreshToken == "" {
		return nil, fmt.Errorf("xai oauth: no refresh token available")
	}

	tokenEndpoint := ""
	if creds.ProviderSpecific != nil {
		tokenEndpoint = strings.TrimSpace(creds.ProviderSpecific["token_endpoint"])
	}
	if tokenEndpoint == "" {
		tokenEndpoint = s.Discover(ctx).TokenEndpoint
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {ClientID},
		"refresh_token": {creds.RefreshToken},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("xai oauth: create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xai oauth: refresh request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xai oauth: read refresh response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("xai oauth: token refresh failed %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("xai oauth: parse refresh response: %w", err)
	}

	newCreds := *creds
	newCreds.AccessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		newCreds.RefreshToken = tokenResp.RefreshToken
	}
	if tokenResp.IDToken != "" {
		newCreds.IDToken = tokenResp.IDToken
		email, sub := extractTokenClaims(tokenResp.IDToken)
		if email != "" {
			newCreds.Email = email
		}
		if sub != "" {
			newCreds.AccountID = sub
		}
	}
	if tokenResp.ExpiresIn > 0 {
		newCreds.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	if newCreds.ProviderSpecific == nil {
		newCreds.ProviderSpecific = map[string]string{}
	}
	newCreds.ProviderSpecific["token_endpoint"] = tokenEndpoint
	if newCreds.IDToken != "" {
		newCreds.ProviderSpecific["idToken"] = newCreds.IDToken
	}
	if newCreds.Email != "" {
		newCreds.ProviderSpecific["email"] = newCreds.Email
	}
	if newCreds.AccountID != "" {
		newCreds.ProviderSpecific["sub"] = newCreds.AccountID
	}

	return &newCreds, nil
}

// StartLocalServer starts a local HTTP server to receive the OAuth callback.
// The xAI OAuth app is registered with the fixed redirect URI
// http://127.0.0.1:56121/callback, so the port is fixed.
func (s *OAuthService) StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error) {
	stateParam := stateParam(state)
	resultChan := make(chan *auth.Credentials, 1)

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

		creds, err := s.exchangeCode(r.Context(), code, RedirectURI, codeVerifier)
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

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", LoopbackPort))
	if err != nil {
		return 0, nil, fmt.Errorf("xai oauth: listen on port %d: %w (is another OAuth flow already running?)", LoopbackPort, err)
	}

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
	return LoopbackPort, resultChan, nil
}

// PKCECodes holds PKCE challenge/verifier pair.
type PKCECodes struct {
	CodeVerifier  string
	CodeChallenge string
}

// generatePKCECodes generates PKCE codes (RFC 7636, S256, 96-byte verifier).
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

// extractTokenClaims extracts email and sub from a JWT ID token.
func extractTokenClaims(idToken string) (email, sub string) {
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
	return claims.Email, claims.Sub
}
