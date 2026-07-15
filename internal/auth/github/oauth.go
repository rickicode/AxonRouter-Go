package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

// githubCopilotClientID is GitHub Copilot CLI's public OAuth App ID.
const githubCopilotClientID = "Iv1.b507a08c87ecfe98"

const (
	defaultDeviceCodeURL     = "https://github.com/login/device/code"
	defaultTokenURL          = "https://github.com/login/oauth/access_token"
	defaultCopilotTokenURL   = "https://api.github.com/copilot_internal/v2/token"
	defaultUserInfoURL       = "https://api.github.com/user"
	defaultScope             = "read:user"
	defaultPollTimeout       = 5 * time.Minute
	defaultPollInterval      = 5 * time.Second
	defaultPostExchangeDelay = 0
)

// deviceCodeResponse is returned by GitHub's device-code endpoint.
type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// tokenResponse is returned by GitHub's access-token endpoint.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// copilotTokenResponse is returned by the Copilot token endpoint.
type copilotTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

// userResponse is returned by the GitHub user endpoint.
type userResponse struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// pendingDeviceCode stores device-flow metadata keyed by state.
type pendingDeviceCode struct {
	VerificationURI string
	UserCode        string
}

// OAuthService handles GitHub's device-code OAuth flow for Copilot.
type OAuthService struct {
	httpClient          *http.Client
	mu                  sync.Mutex
	pending             map[string]*pendingDeviceCode
	deviceCodeURL       string
	tokenURL            string
	copilotTokenURL     string
	userInfoURL         string
	defaultPollTimeout  time.Duration
	defaultPollInterval time.Duration
	postExchangeDelay   time.Duration
}

// NewOAuthService creates a new GitHub OAuth service.
func NewOAuthService(httpClient *http.Client) *OAuthService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &OAuthService{
		httpClient:          httpClient,
		pending:             make(map[string]*pendingDeviceCode),
		deviceCodeURL:       defaultDeviceCodeURL,
		tokenURL:            defaultTokenURL,
		copilotTokenURL:     defaultCopilotTokenURL,
		userInfoURL:         defaultUserInfoURL,
		defaultPollTimeout:  defaultPollTimeout,
		defaultPollInterval: defaultPollInterval,
		postExchangeDelay:   defaultPostExchangeDelay,
	}
}

// StartLocalServer starts the GitHub device-code flow.
// Device code flow does not use a local callback, so port is always 0.
func (s *OAuthService) StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error) {
	bareState := state
	if idx := strings.LastIndex(state, ":"); idx > 0 {
		bareState = state[:idx]
	}

	deviceCode, err := s.requestDeviceCode(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("request device code: %w", err)
	}

	s.mu.Lock()
	s.pending[bareState] = &pendingDeviceCode{
		VerificationURI: deviceCode.VerificationURI,
		UserCode:        deviceCode.UserCode,
	}
	s.mu.Unlock()

	resultChan := make(chan *auth.Credentials, 1)
	go func() {
		defer close(resultChan)
		time.Sleep(s.postExchangeDelay)
		creds, err := s.pollForToken(ctx, deviceCode.DeviceCode, deviceCode.Interval)
		if err != nil {
			return
		}
		if creds != nil {
			resultChan <- creds
		}
	}()

	return 0, resultChan, nil
}

// GenerateAuthURL returns the verification URI for the pending device code request.
func (s *OAuthService) GenerateAuthURL(_ context.Context, state string) (string, error) {
	bareState := state
	if idx := strings.LastIndex(state, ":"); idx > 0 {
		bareState = state[:idx]
	}

	s.mu.Lock()
	pending := s.pending[bareState]
	s.mu.Unlock()

	if pending == nil {
		return "", fmt.Errorf("no pending device code for state")
	}
	return pending.VerificationURI, nil
}

// GetUserCode returns the device code for the frontend to display. It is keyed
// by the same bare state used by GenerateAuthURL, matching the userCoder
// interface expected by the admin OAuth handler.
func (s *OAuthService) GetUserCode(state string) string {
	bareState := state
	if idx := strings.LastIndex(state, ":"); idx > 0 {
		bareState = state[:idx]
	}

	s.mu.Lock()
	pending := s.pending[bareState]
	delete(s.pending, bareState)
	s.mu.Unlock()

	if pending == nil {
		return ""
	}
	return pending.UserCode
}

// ExchangeCode is not used by GitHub's device-code flow.
func (s *OAuthService) ExchangeCode(_ context.Context, _ string) (*auth.Credentials, error) {
	return nil, fmt.Errorf("GitHub uses device-code flow; authorization code exchange is not used")
}

// RefreshToken re-fetches the short-lived Copilot token using the stored GitHub
// access token. GitHub's device-code access token itself does not expire, but
// the Copilot token does; this keeps the connection eligible without disabling
// it every time the Copilot token nears expiry.
func (s *OAuthService) RefreshToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	if creds.AccessToken == "" {
		return nil, fmt.Errorf("no access token available")
	}
	copilot, err := s.fetchCopilotToken(ctx, creds.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("refresh copilot token: %w", err)
	}
	newCreds := *creds
	if newCreds.ProviderSpecific == nil {
		newCreds.ProviderSpecific = map[string]string{}
	}
	newCreds.ProviderSpecific["copilotToken"] = copilot.Token
	newCreds.ProviderSpecific["copilotTokenExpiresAt"] = strconv.FormatInt(copilot.ExpiresAt, 10)
	if copilot.ExpiresAt > 0 {
		newCreds.ExpiresAt = time.Unix(copilot.ExpiresAt, 0)
	}
	return &newCreds, nil
}

func (s *OAuthService) requestDeviceCode(ctx context.Context) (*deviceCodeResponse, error) {
	data := url.Values{
		"client_id": {githubCopilotClientID},
		"scope":     {defaultScope},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.deviceCodeURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create device-code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device-code request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read device-code response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device-code request failed %d: %s", resp.StatusCode, string(body))
	}

	var result deviceCodeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse device-code response: %w", err)
	}
	if result.DeviceCode == "" || result.UserCode == "" {
		return nil, fmt.Errorf("device-code response missing required fields")
	}
	return &result, nil
}

func (s *OAuthService) pollForToken(ctx context.Context, deviceCode string, intervalSeconds int) (*auth.Credentials, error) {
	interval := s.defaultPollInterval
	if intervalSeconds > 0 {
		interval = time.Duration(intervalSeconds) * time.Second
	}
	deadline := time.Now().Add(s.defaultPollTimeout)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("device authentication timed out after %v", s.defaultPollTimeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}

		token, err := s.exchangeDeviceCode(ctx, deviceCode)
		if err != nil {
			low := strings.ToLower(err.Error())
			if strings.Contains(low, "authorization_pending") || strings.Contains(low, "slow_down") {
				if strings.Contains(low, "slow_down") {
					interval += time.Second
				}
				continue
			}
			return nil, err
		}

		creds, err := s.fetchCopilotAndUser(ctx, token)
		if err != nil {
			return nil, err
		}
		return creds, nil
	}
}

func (s *OAuthService) exchangeDeviceCode(ctx context.Context, deviceCode string) (*tokenResponse, error) {
	data := url.Values{
		"client_id":   {githubCopilotClientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

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

	var result tokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	if result.AccessToken == "" {
		return nil, fmt.Errorf("empty access token")
	}
	return &result, nil
}

func (s *OAuthService) fetchCopilotAndUser(ctx context.Context, token *tokenResponse) (*auth.Credentials, error) {
	copilot, err := s.fetchCopilotToken(ctx, token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("fetch copilot token: %w", err)
	}
	user, _ := s.fetchUserInfo(ctx, token.AccessToken)

	creds := &auth.Credentials{
		AccessToken:    token.AccessToken,
		RefreshToken:   token.RefreshToken,
		ProviderSpecific: map[string]string{
			"copilotToken":          copilot.Token,
			"copilotTokenExpiresAt": strconv.FormatInt(copilot.ExpiresAt, 10),
			"githubUserId":          fmt.Sprintf("%d", user.ID),
			"githubLogin":           user.Login,
			"githubName":            user.Name,
			"githubEmail":           user.Email,
		},
	}
	if copilot.ExpiresAt > 0 {
		creds.ExpiresAt = time.Unix(copilot.ExpiresAt, 0)
	}
	if token.ExpiresIn > 0 {
		creds.ExpiresAt = time.Now().Add(time.Duration(token.ExpiresIn) * time.Second)
	}
	return creds, nil
}

func (s *OAuthService) fetchCopilotToken(ctx context.Context, accessToken string) (*copilotTokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.copilotTokenURL, nil)
	if err != nil {
		return nil, err
	}
	// GitHub's copilot_internal endpoints expect the OAuth token in "token" form,
	// not the Bearer form used by api.github.com/user.
	req.Header.Set("Authorization", "token "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copilot token request failed %d: %s", resp.StatusCode, string(body))
	}

	var result copilotTokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *OAuthService) fetchUserInfo(ctx context.Context, accessToken string) (*userResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.userInfoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("user info request failed %d: %s", resp.StatusCode, string(body))
	}

	var result userResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
