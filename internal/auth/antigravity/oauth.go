package antigravity

import (
	"bytes"
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

// OAuth configuration for Antigravity (Google Cloud Code Assist).
// Values match OmniRoute's resolvePublicCred("antigravity_*") defaults.
const (
	AuthURL      = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenURL     = "https://oauth2.googleapis.com/token"
	UserInfoURL  = "https://www.googleapis.com/oauth2/v1/userinfo"
	ClientID     = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	ClientSecret = "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"
	RedirectURI  = "http://localhost:%d/callback"
	Scopes       = "openid https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile https://www.googleapis.com/auth/cclog https://www.googleapis.com/auth/experimentsandconfigs"
)

var antigravityBaseURLs = []string{
	"https://daily-cloudcode-pa.googleapis.com",
	"https://cloudcode-pa.googleapis.com",
	"https://daily-cloudcode-pa.sandbox.googleapis.com",
}

// OAuthService handles Google OAuth flow for Antigravity.
type OAuthService struct {
	httpClient *http.Client
	mu         sync.Mutex
	pkce       map[string]string
}

// NewOAuthService creates a new Antigravity OAuth service.
func NewOAuthService(httpClient *http.Client) *OAuthService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &OAuthService{httpClient: httpClient, pkce: make(map[string]string)}
}

// GenerateAuthURL creates the Google OAuth authorization URL.
func (s *OAuthService) GenerateAuthURL(ctx context.Context, state string) (string, error) {
	parts := strings.SplitN(state, ":", 2)
	stateParam := parts[0]
	port := 1456
	if len(parts) > 1 {
		fmt.Sscanf(parts[1], "%d", &port)
	}

	redirectURI := fmt.Sprintf(RedirectURI, port)
	pkce, err := GeneratePKCECodes()
	if err != nil {
		return "", fmt.Errorf("generate PKCE: %w", err)
	}
	s.mu.Lock()
	s.pkce[stateParam] = pkce.CodeVerifier
	s.mu.Unlock()

	params := url.Values{
		"client_id":             {ClientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {Scopes},
		"state":                 {stateParam},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
		"code_challenge":        {pkce.CodeChallenge},
		"code_challenge_method": {"S256"},
	}

	return fmt.Sprintf("%s?%s", AuthURL, params.Encode()), nil
}

// ExchangeCode exchanges an authorization code for tokens.
func (s *OAuthService) ExchangeCode(ctx context.Context, code string) (*auth.Credentials, error) {
	return s.exchangeCode(ctx, code, fmt.Sprintf(RedirectURI, 1456), "")
}

func (s *OAuthService) exchangeCode(ctx context.Context, code, redirectURI, codeVerifier string) (*auth.Credentials, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {ClientID},
		"client_secret": {ClientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}
	if codeVerifier != "" {
		data.Set("code_verifier", codeVerifier)
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
		Scope        string `json:"scope"`
	}

	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	email := ""
	if tokenResp.IDToken != "" {
		email = extractGoogleEmail(tokenResp.IDToken)
	}
	providerSpecific, _ := s.postExchange(ctx, tokenResp.AccessToken)
	if email == "" && providerSpecific["email"] != "" {
		email = providerSpecific["email"]
	}
	if email != "" {
		providerSpecific["email"] = email
	}

	return &auth.Credentials{
		AccessToken:      tokenResp.AccessToken,
		RefreshToken:     tokenResp.RefreshToken,
		IDToken:          tokenResp.IDToken,
		Email:            email,
		ExpiresAt:        time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		ProviderSpecific: providerSpecific,
	}, nil
}

// RefreshToken refreshes an expired Google access token.
func (s *OAuthService) RefreshToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	if creds.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {ClientID},
		"client_secret": {ClientSecret},
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
	var port int

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

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, nil, fmt.Errorf("listen: %w", err)
	}
	port = listener.Addr().(*net.TCPAddr).Port

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

func (s *OAuthService) postExchange(ctx context.Context, accessToken string) (map[string]string, error) {
	out := map[string]string{}
	userInfoReq, _ := http.NewRequestWithContext(ctx, "GET", UserInfoURL+"?alt=json", nil)
	userInfoReq.Header.Set("Authorization", "Bearer "+accessToken)
	if resp, err := s.httpClient.Do(userInfoReq); err == nil {
		func() {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var userInfo struct {
					Email string `json:"email"`
				}
				if body, err := io.ReadAll(resp.Body); err == nil && json.Unmarshal(body, &userInfo) == nil && userInfo.Email != "" {
					out["email"] = userInfo.Email
				}
			}
		}()
	}

	headers := map[string]string{
		"Content-Type":  "application/json",
		"User-Agent":    "vscode/1.X.X (Antigravity/4.2.0)",
		"Authorization": "Bearer " + accessToken,
	}
	body := []byte(`{"metadata":{"ideType":"ANTIGRAVITY"}}`)

	projectID := ""
	tierID := "legacy-tier"
	for _, baseURL := range antigravityBaseURLs {
		req, _ := http.NewRequestWithContext(ctx, "POST", baseURL+"/v1internal:loadCodeAssist", bytes.NewReader(body))
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := s.httpClient.Do(req)
		if err != nil {
			continue
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return
			}
			var data map[string]any
			if b, err := io.ReadAll(resp.Body); err == nil && json.Unmarshal(b, &data) == nil {
				projectID = pickProjectID(data)
				tierID = pickTierID(data)
			}
		}()
		if projectID != "" {
			break
		}
	}

	if projectID != "" {
		for _, baseURL := range antigravityBaseURLs {
			reqBody := []byte(fmt.Sprintf(`{"tier_id":%q,"metadata":{"ideType":"ANTIGRAVITY"}}`, tierID))
			req, _ := http.NewRequestWithContext(ctx, "POST", baseURL+"/v1internal:onboardUser", bytes.NewReader(reqBody))
			for k, v := range headers {
				req.Header.Set(k, v)
			}
			resp, err := s.httpClient.Do(req)
			if err != nil {
				continue
			}
			resp.Body.Close()
			if resp.StatusCode < 500 {
				break
			}
		}
		out["projectId"] = projectID
		out["tier"] = tierID
	}

	return out, nil
}

func pickProjectID(data map[string]any) string {
	if v, ok := data["cloudaicompanionProject"].(string); ok {
		return strings.TrimSpace(v)
	}
	if obj, ok := data["cloudaicompanionProject"].(map[string]any); ok {
		if id, ok := obj["id"].(string); ok {
			return strings.TrimSpace(id)
		}
	}
	return ""
}

func pickTierID(data map[string]any) string {
	sub, _ := data["subscriptionInfo"].(map[string]any)
	for _, key := range []string{"paidTier", "currentTier"} {
		if tier, ok := sub[key].(map[string]any); ok {
			if id, ok := tier["id"].(string); ok && strings.TrimSpace(id) != "" {
				return strings.TrimSpace(id)
			}
		}
	}
	return "legacy-tier"
}

func GeneratePKCECodes() (*PKCECodes, error) {
	bytes := make([]byte, 96)
	if _, err := rand.Read(bytes); err != nil {
		return nil, fmt.Errorf("generate random: %w", err)
	}
	verifier := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(bytes)
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(hash[:])
	return &PKCECodes{CodeVerifier: verifier, CodeChallenge: challenge}, nil
}

type PKCECodes struct {
	CodeVerifier  string
	CodeChallenge string
}

// extractGoogleEmail extracts email from a Google ID token.
func extractGoogleEmail(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return ""
	}

	// ponytail: manual base64 decode, no JWT lib needed
	payload := parts[1]
	// Add padding if needed
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}

	// URL-safe base64 decode
	payload = strings.ReplaceAll(payload, "-", "+")
	payload = strings.ReplaceAll(payload, "_", "/")

	var claims struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal([]byte(payload), &claims); err != nil {
		return ""
	}
	return claims.Email
}
