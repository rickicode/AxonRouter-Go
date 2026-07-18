package grokcli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

// OAuthService handles xAI Grok CLI OAuth2 device-code flow.
type OAuthService struct {
	httpClient   *http.Client
	discoveryURL string

	// testDiscoveryResponse, when set, is returned by Discover so tests can
	// avoid calling the real xAI OIDC discovery endpoint.
	testDiscoveryResponse *Discovery

	// testMinPollInterval overrides defaultPollInterval in tests.
	testMinPollInterval time.Duration

	mu     sync.Mutex
	states map[string]*deviceFlowState
}

type deviceFlowState struct {
	userCode        string
	verificationURI string
}

// NewOAuthService creates a new Grok CLI OAuth service.
func NewOAuthService(httpClient *http.Client) *OAuthService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: httpClientTimeout}
	}
	return &OAuthService{
		httpClient:   httpClient,
		discoveryURL: DiscoveryURL,
		states:       make(map[string]*deviceFlowState),
	}
}

// StartLocalServer runs OIDC discovery, requests a device code, and starts polling
// for authorization in the background. It returns a buffered channel that will
// receive the credentials once the user completes the flow.
func (s *OAuthService) StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error) {
	stateParam := stateParamFromState(state)

	discovery, err := s.Discover(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("grokcli oauth: discovery failed: %w", err)
	}

	deviceCode, err := s.requestDeviceCode(ctx, discovery.DeviceAuthorizationEndpoint, discovery.TokenEndpoint)
	if err != nil {
		return 0, nil, fmt.Errorf("grokcli oauth: device code request failed: %w", err)
	}

	s.mu.Lock()
	s.states[stateParam] = &deviceFlowState{
		userCode:        deviceCode.UserCode,
		verificationURI: deviceCode.VerificationURI,
	}
	s.mu.Unlock()

	resultChan := make(chan *auth.Credentials, 1)
	go s.pollAndSend(ctx, deviceCode, stateParam, resultChan)

	return 0, resultChan, nil
}

func (s *OAuthService) pollAndSend(ctx context.Context, deviceCode *DeviceCodeResponse, stateParam string, resultChan chan *auth.Credentials) {
	creds, err := s.pollForToken(ctx, deviceCode)

	s.mu.Lock()
	delete(s.states, stateParam)
	s.mu.Unlock()

	if err != nil {
		resultChan <- &auth.Credentials{
			ProviderSpecific: map[string]string{
				"__oauth_error__": err.Error(),
			},
		}
		return
	}

	resultChan <- creds
}

// GenerateAuthURL returns the verification URI stored by StartLocalServer.
func (s *OAuthService) GenerateAuthURL(ctx context.Context, state string) (string, error) {
	stateParam := stateParamFromState(state)

	s.mu.Lock()
	st := s.states[stateParam]
	s.mu.Unlock()
	if st == nil {
		return "", fmt.Errorf("grokcli oauth: no pending device flow for state")
	}
	if st.verificationURI == "" {
		return "", fmt.Errorf("grokcli oauth: verification URI not available")
	}
	return st.verificationURI, nil
}

// GetUserCode returns the user code for the dashboard.
func (s *OAuthService) GetUserCode(state string) string {
	stateParam := stateParamFromState(state)

	s.mu.Lock()
	st := s.states[stateParam]
	s.mu.Unlock()
	if st == nil {
		return ""
	}
	return st.userCode
}

// ExchangeCode is not supported for the device-code-only Grok CLI provider.
func (s *OAuthService) ExchangeCode(ctx context.Context, code string) (*auth.Credentials, error) {
	return nil, fmt.Errorf("grok-cli uses device-code flow; authorization-code exchange is not supported")
}

// RefreshToken refreshes an expired access token using the stored token_endpoint.
func (s *OAuthService) RefreshToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	if creds.RefreshToken == "" {
		return nil, fmt.Errorf("grokcli oauth: no refresh token available")
	}

	tokenEndpoint := ""
	if creds.ProviderSpecific != nil {
		tokenEndpoint = strings.TrimSpace(creds.ProviderSpecific["token_endpoint"])
	}
	if tokenEndpoint == "" {
		discovery, err := s.Discover(ctx)
		if err != nil {
			return nil, err
		}
		tokenEndpoint = discovery.TokenEndpoint
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {ClientID},
		"refresh_token": {creds.RefreshToken},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("grokcli refresh: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("grokcli refresh: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("grokcli refresh: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("grokcli refresh failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("grokcli refresh: parse response: %w", err)
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return nil, fmt.Errorf("grokcli refresh: response missing access_token")
	}

	newCreds := *creds
	newCreds.AccessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		newCreds.RefreshToken = tokenResp.RefreshToken
	}
	if tokenResp.IDToken != "" {
		newCreds.IDToken = tokenResp.IDToken
		email, sub := parseJWTIdentity(tokenResp.IDToken)
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
	psd := make(map[string]string, len(newCreds.ProviderSpecific)+4)
	for k, v := range newCreds.ProviderSpecific {
		psd[k] = v
	}
	newCreds.ProviderSpecific = psd
	newCreds.ProviderSpecific["token_endpoint"] = tokenEndpoint
	if newCreds.Email != "" {
		newCreds.ProviderSpecific["email"] = newCreds.Email
	}
	if newCreds.AccountID != "" {
		newCreds.ProviderSpecific["sub"] = newCreds.AccountID
	}
	if strings.TrimSpace(tokenResp.TokenType) != "" {
		newCreds.ProviderSpecific["token_type"] = strings.TrimSpace(tokenResp.TokenType)
	}

	return &newCreds, nil
}

func stateParamFromState(state string) string {
	parts := strings.SplitN(state, ":", 2)
	return parts[0]
}
