package codebuddy

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

// OAuthService handles CodeBuddy's custom Tencent OAuth polling flow.
type OAuthService struct {
	httpClient *http.Client

	// URL overrides default to the production constants; tests can point them
	// at an httptest server.
	stateURL   string
	tokenURL   string
	refreshURL string

	// testPollInterval overrides pollInterval in tests.
	testPollInterval time.Duration

	mu     sync.Mutex
	states map[string]*deviceFlowState
}

// NewOAuthService creates a new CodeBuddy OAuth service.
func NewOAuthService(httpClient *http.Client) *OAuthService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: httpClientTimeout}
	}
	return &OAuthService{
		httpClient: httpClient,
		stateURL:   stateURL,
		tokenURL:   tokenURL,
		refreshURL: refreshURL,
		states:     make(map[string]*deviceFlowState),
	}
}

// StartLocalServer requests a device code and starts polling for authorization.
// It returns port 0 because CodeBuddy does not use a local callback server.
func (s *OAuthService) StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error) {
	stateParam := stateParamFromState(state)

	dc, err := s.requestDeviceCode(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("codebuddy oauth: %w", err)
	}

	s.mu.Lock()
	s.states[stateParam] = &deviceFlowState{
		state:   dc.state,
		authUrl: dc.authUrl,
	}
	s.mu.Unlock()

	resultChan := make(chan *auth.Credentials, 1)
	go s.pollAndSend(ctx, dc.state, stateParam, resultChan)

	return 0, resultChan, nil
}

func (s *OAuthService) pollAndSend(ctx context.Context, pollState, stateParam string, resultChan chan *auth.Credentials) {
	creds, err := s.pollToken(ctx, pollState)

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

// GenerateAuthURL returns the stored authorization URL for the user to open.
func (s *OAuthService) GenerateAuthURL(ctx context.Context, state string) (string, error) {
	stateParam := stateParamFromState(state)

	s.mu.Lock()
	st := s.states[stateParam]
	s.mu.Unlock()

	if st == nil {
		return "", fmt.Errorf("codebuddy oauth: no pending device flow for state")
	}
	if st.authUrl == "" {
		return "", fmt.Errorf("codebuddy oauth: auth URL not available")
	}
	return st.authUrl, nil
}

// GetUserCode returns the state string displayed as the user code.
func (s *OAuthService) GetUserCode(state string) string {
	stateParam := stateParamFromState(state)

	s.mu.Lock()
	st := s.states[stateParam]
	s.mu.Unlock()

	if st == nil {
		return ""
	}
	return st.state
}

// ExchangeCode is not supported for CodeBuddy's device-code-style flow.
func (s *OAuthService) ExchangeCode(ctx context.Context, code string) (*auth.Credentials, error) {
	return nil, fmt.Errorf("codebuddy uses a custom device-code flow; authorization-code exchange is not supported")
}

// RefreshToken refreshes an access token using the X-Refresh-Token header.
func (s *OAuthService) RefreshToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	if creds.RefreshToken == "" {
		return nil, fmt.Errorf("codebuddy oauth: no refresh token available")
	}

	body := strings.NewReader("{}")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.refreshURL, body)
	if err != nil {
		return nil, fmt.Errorf("codebuddy refresh: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-Domain", "www.codebuddy.ai")
	req.Header.Set("X-Refresh-Token", creds.RefreshToken)
	req.Header.Set("X-Auth-Refresh-Source", "plugin")
	req.Header.Set("X-Product", "SaaS")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("codebuddy refresh: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("codebuddy refresh: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("codebuddy refresh failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var tokenRes tokenResponse
	if err := json.Unmarshal(respBody, &tokenRes); err != nil {
		return nil, fmt.Errorf("codebuddy refresh: parse response: %w", err)
	}
	if tokenRes.Code != 0 {
		return nil, fmt.Errorf("codebuddy refresh error (code %d): %s", tokenRes.Code, tokenRes.Msg)
	}
	if strings.TrimSpace(tokenRes.Data.AccessToken) == "" {
		return nil, fmt.Errorf("codebuddy refresh: response missing accessToken")
	}

	newCreds := *creds
	newCreds.AccessToken = strings.TrimSpace(tokenRes.Data.AccessToken)
	if tokenRes.Data.RefreshToken != "" {
		newCreds.RefreshToken = tokenRes.Data.RefreshToken
	}
	if tokenRes.Data.ExpiresIn > 0 {
		newCreds.ExpiresAt = time.Now().Add(time.Duration(tokenRes.Data.ExpiresIn) * time.Second)
	}

	return &newCreds, nil
}

func (s *OAuthService) requestDeviceCode(ctx context.Context) (*deviceCodeResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	stateURL, err := url.Parse(s.stateURL)
	if err != nil {
		return nil, fmt.Errorf("parse state URL: %w", err)
	}
	q := stateURL.Query()
	q.Set("platform", platform)
	stateURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, stateURL.String(), strings.NewReader("{}"))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-Domain", "www.codebuddy.ai")
	req.Header.Set("X-No-Authorization", "true")
	req.Header.Set("X-No-User-Id", "true")
	req.Header.Set("X-Product", "SaaS")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("state request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("state request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var stateRes stateResponse
	if err := json.Unmarshal(respBody, &stateRes); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if stateRes.Code != 0 {
		return nil, fmt.Errorf("state error (code %d): %s", stateRes.Code, stateRes.Msg)
	}
	if strings.TrimSpace(stateRes.Data.State) == "" {
		return nil, fmt.Errorf("state response missing state")
	}

	return &deviceCodeResponse{
		state:   strings.TrimSpace(stateRes.Data.State),
		authUrl: strings.TrimSpace(stateRes.Data.AuthUrl),
	}, nil
}

// enrichCredentials fetches the CodeBuddy account profile and payment/plan type
// using the access token, then fills Email and ProviderSpecific fields.
func (s *OAuthService) enrichCredentials(ctx context.Context, creds *auth.Credentials, state string) error {
	if creds == nil || creds.AccessToken == "" {
		return nil
	}

	if creds.ProviderSpecific == nil {
		creds.ProviderSpecific = map[string]string{}
	}

	account, err := s.fetchAccount(ctx, creds.AccessToken)
	if err != nil {
		return err
	}

	email := strings.TrimSpace(account.Nickname)
	if email == "" {
		email = strings.TrimSpace(account.UID)
	}
	if email != "" && creds.Email == "" {
		creds.Email = email
	}
	if account.UID != "" {
		creds.ProviderSpecific["uid"] = account.UID
	}
	if account.Type != "" {
		creds.ProviderSpecific["account_type"] = account.Type
	}
	if account.EnterpriseID != "" {
		creds.ProviderSpecific["enterprise_id"] = account.EnterpriseID
	}
	if account.EnterpriseName != "" {
		creds.ProviderSpecific["enterprise_name"] = account.EnterpriseName
	}

	paymentType, err := s.fetchPaymentType(ctx, creds.AccessToken, account.UID, account.EnterpriseID)
	if err == nil && paymentType != "" {
		creds.ProviderSpecific["plan_type"] = paymentType
	}

	return nil
}

func (s *OAuthService) fetchAccount(ctx context.Context, accessToken string) (codebuddyAccount, error) {
	var empty codebuddyAccount
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.codebuddy.ai/v2/plugin/accounts", nil)
	if err != nil {
		return empty, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("X-Domain", "www.codebuddy.ai")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return empty, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return empty, err
	}
	if resp.StatusCode != http.StatusOK {
		return empty, fmt.Errorf("accounts fetch failed %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var accountsRes accountsResponse
	if err := json.Unmarshal(body, &accountsRes); err != nil {
		return empty, err
	}
	if accountsRes.Code != 0 {
		return empty, fmt.Errorf("accounts fetch error (code %d): %s", accountsRes.Code, accountsRes.Msg)
	}

	for _, a := range accountsRes.Data.Accounts {
		if a.LastLogin {
			return a, nil
		}
	}
	if len(accountsRes.Data.Accounts) > 0 {
		return accountsRes.Data.Accounts[0], nil
	}
	return empty, fmt.Errorf("no accounts returned")
}

func (s *OAuthService) fetchPaymentType(ctx context.Context, accessToken, uid, enterpriseID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://www.codebuddy.ai/v2/billing/meter/get-payment-type", strings.NewReader("{}"))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Domain", "www.codebuddy.ai")
	if uid != "" {
		req.Header.Set("X-User-Id", uid)
	}
	if enterpriseID != "" {
		req.Header.Set("X-Enterprise-Id", enterpriseID)
		req.Header.Set("X-Tenant-Id", enterpriseID)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("payment type fetch failed %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var paymentRes paymentTypeResponse
	if err := json.Unmarshal(body, &paymentRes); err != nil {
		return "", err
	}
	if paymentRes.Code != 0 {
		return "", fmt.Errorf("payment type fetch error (code %d): %s", paymentRes.Code, paymentRes.Msg)
	}
	return strings.TrimSpace(paymentRes.Data.PaymentType), nil
}

func (s *OAuthService) pollToken(ctx context.Context, state string) (*auth.Credentials, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	interval := pollInterval
	if s.testPollInterval > 0 {
		interval = s.testPollInterval
	}

	deadline := time.Now().Add(maxPollDuration)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	tokenURL, err := url.Parse(s.tokenURL)
	if err != nil {
		return nil, fmt.Errorf("parse token URL: %w", err)
	}
	q := tokenURL.Query()
	q.Set("state", state)
	tokenURL.RawQuery = q.Encode()

	timer := time.NewTimer(0)
	defer timer.Stop()

	firstAttempt := true
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled: %w", ctx.Err())
		case <-timer.C:
			if !firstAttempt && time.Now().After(deadline) {
				return nil, fmt.Errorf("device code expired")
			}
			firstAttempt = false

			creds, pending, pollErr := s.pollTokenOnce(ctx, tokenURL.String())
			if creds != nil {
				// Best-effort: fetch profile email and plan type. Failures are logged
				// but do not block the OAuth flow.
				if err := s.enrichCredentials(ctx, creds, state); err != nil {
					_ = err
				}
				return creds, nil
			}
			if pollErr != nil {
				return nil, pollErr
			}
			if !pending {
				return nil, fmt.Errorf("device code authorization failed")
			}
			timer.Reset(interval)
		}
	}
}

func (s *OAuthService) pollTokenOnce(ctx context.Context, tokenURL string) (*auth.Credentials, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-Domain", "www.codebuddy.ai")
	req.Header.Set("X-No-Authorization", "true")
	req.Header.Set("X-No-User-Id", "true")
	req.Header.Set("X-No-Enterprise-Id", "true")
	req.Header.Set("X-No-Department-Info", "true")
	req.Header.Set("X-Product", "SaaS")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("token poll failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("token poll failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenRes tokenResponse
	if err := json.Unmarshal(body, &tokenRes); err != nil {
		return nil, false, fmt.Errorf("parse response: %w", err)
	}

	if tokenRes.Code == 0 && strings.TrimSpace(tokenRes.Data.AccessToken) != "" {
		creds := &auth.Credentials{
			AccessToken:  strings.TrimSpace(tokenRes.Data.AccessToken),
			RefreshToken: strings.TrimSpace(tokenRes.Data.RefreshToken),
		}
		if email := strings.TrimSpace(tokenRes.Data.Email); email != "" {
			creds.Email = email
		}
		if tokenRes.Data.TokenType != "" {
			if creds.ProviderSpecific == nil {
				creds.ProviderSpecific = map[string]string{}
			}
			creds.ProviderSpecific["token_type"] = tokenRes.Data.TokenType
		}
		if tokenRes.Data.ExpiresIn > 0 {
			creds.ExpiresAt = time.Now().Add(time.Duration(tokenRes.Data.ExpiresIn) * time.Second)
		}
		return creds, false, nil
	}

	// 11217 = RetryFetchToken (pending)
	if tokenRes.Code == 11217 {
		return nil, true, nil
	}

	return nil, false, fmt.Errorf("token poll error (code %d): %s", tokenRes.Code, tokenRes.Msg)
}

func stateParamFromState(state string) string {
	parts := strings.SplitN(state, ":", 2)
	return parts[0]
}
