package kiro

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

const (
	registerClientURL = "https://oidc.us-east-1.amazonaws.com/client/register"
	deviceAuthURL     = "https://oidc.us-east-1.amazonaws.com/device_authorization"
	tokenURL          = "https://oidc.us-east-1.amazonaws.com/token"
	clientName        = "kiro-oauth-client"
	clientType        = "public"
	startURL          = "https://view.awsapps.com/start"
	issuerURL         = "https://identitycenter.amazonaws.com/ssoins-722374e8c3c8e6c6"
	pollInterval      = 5 * time.Second
	pollTimeout       = 5 * time.Minute
)
var scopes = []string{
	"codewhisperer:completions",
	"codewhisperer:analysis",
	"codewhisperer:conversations",
}

var grantTypes = []string{
	"urn:ietf:params:oauth:grant-type:device_code",
	"refresh_token",
}

// DeviceCodeResult holds device code metadata for the handler to read.
type DeviceCodeResult struct {
	VerificationURI string
	UserCode        string
}

// OAuthService handles AWS device code flow for Kiro.
type OAuthService struct {
	httpClient *http.Client
	// pending stores device code metadata keyed by state, written by
	// StartLocalServer, read by GenerateAuthURL.
	pending sync.Map
}

func NewOAuthService(httpClient *http.Client) *OAuthService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &OAuthService{httpClient: httpClient}
}

// StartLocalServer registers an OIDC client, starts device authorization,
// and begins polling for the token. Stores verification URL + user code
// for GenerateAuthURL to return.
func (s *OAuthService) StartLocalServer(ctx context.Context, state string) (int, chan *auth.Credentials, error) {
	clientID, clientSecret, err := s.registerClient(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("register client: %w", err)
	}

	verificationURI, userCode, deviceCode, err := s.startDeviceAuthorization(ctx, clientID, clientSecret)
	if err != nil {
		return 0, nil, fmt.Errorf("start device auth: %w", err)
	}

	// Store metadata for GenerateAuthURL
	s.pending.Store(state, DeviceCodeResult{
		VerificationURI: verificationURI,
		UserCode:        userCode,
	})

	resultChan := make(chan *auth.Credentials, 1)
	go func() {
		defer close(resultChan)
		deadline := time.Now().Add(pollTimeout)
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(pollInterval):
			}
			creds, err := s.pollDeviceToken(ctx, clientID, clientSecret, deviceCode)
			if err != nil {
				msg := err.Error()
				if strings.Contains(msg, "slow_down") {
					time.Sleep(pollInterval)
					continue
				}
				if strings.Contains(msg, "authorization_pending") {
					continue
				}
				return
			}
			if creds != nil {
				resultChan <- creds
				return
			}
		}
	}()

	return 0, resultChan, nil
}

// GenerateAuthURL reads the device code metadata stored by StartLocalServer
// and returns the verification URL.
func (s *OAuthService) GenerateAuthURL(_ context.Context, state string) (string, error) {
	bare := state
	if idx := strings.LastIndex(state, ":"); idx > 0 {
		bare = state[:idx]
	}
	val, ok := s.pending.Load(bare)
	if !ok {
		return "", fmt.Errorf("no pending device code for state")
	}
	result := val.(DeviceCodeResult)
	return result.VerificationURI, nil
}
func (s *OAuthService) GetUserCode(state string) string {
	val, ok := s.pending.LoadAndDelete(state)
	if !ok {
		return ""
	}
	return val.(DeviceCodeResult).UserCode
}

func (s *OAuthService) ExchangeCode(_ context.Context, _ string) (*auth.Credentials, error) {
	return nil, fmt.Errorf("kiro uses device code flow, not authorization code exchange")
}

func (s *OAuthService) RefreshToken(ctx context.Context, creds *auth.Credentials) (*auth.Credentials, error) {
	if creds.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}
	body := fmt.Sprintf(`{"refreshToken":%q}`, creds.RefreshToken)
	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken",
		strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh failed %d: %s", resp.StatusCode, string(respBody))
	}

	var tokenResp struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int64  `json:"expiresIn"`
	}
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return nil, err
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("empty access token")
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

// --- AWS SSO OIDC helpers ---

type registerClientRequest struct {
	ClientName string   `json:"clientName"`
	ClientType string   `json:"clientType"`
	Scopes     []string `json:"scopes"`
	GrantTypes []string `json:"grantTypes"`
	IssuerURL  string   `json:"issuerUrl"`
}

func (s *OAuthService) registerClient(ctx context.Context) (clientID, clientSecret string, err error) {
	reqBody := registerClientRequest{
		ClientName: clientName,
		ClientType: clientType,
		Scopes:     scopes,
		GrantTypes: grantTypes,
		IssuerURL:  issuerURL,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", registerClientURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("register failed %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", err
	}
	if result.ClientID == "" {
		return "", "", fmt.Errorf("empty clientId")
	}
	return result.ClientID, result.ClientSecret, nil
}

type deviceAuthRequest struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	StartURL     string `json:"startUrl"`
}

func (s *OAuthService) startDeviceAuthorization(ctx context.Context, clientID, clientSecret string) (verificationURI, userCode, deviceCode string, err error) {
	reqBody := deviceAuthRequest{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		StartURL:     startURL,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", deviceAuthURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("device auth failed %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		DeviceCode      string `json:"deviceCode"`
		UserCode        string `json:"userCode"`
		VerificationURI string `json:"verificationUri"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", "", err
	}
	if result.VerificationURI == "" || result.DeviceCode == "" {
		return "", "", "", fmt.Errorf("missing verification_uri or device_code")
	}
	return result.VerificationURI, result.UserCode, result.DeviceCode, nil
}

type pollTokenRequest struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	DeviceCode   string `json:"deviceCode"`
	GrantType    string `json:"grantType"`
}

func (s *OAuthService) pollDeviceToken(ctx context.Context, clientID, clientSecret, deviceCode string) (*auth.Credentials, error) {
	reqBody := pollTokenRequest{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		DeviceCode:   deviceCode,
		GrantType:    "urn:ietf:params:oauth:grant-type:device_code",
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusBadRequest {
		var errResp struct {
			Error string `json:"error"`
		}
		json.Unmarshal(respBody, &errResp)
		if errResp.Error != "" {
			return nil, fmt.Errorf("%s", errResp.Error)
		}
		return nil, fmt.Errorf("authorization pending")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("poll failed %d: %s", resp.StatusCode, string(respBody))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return nil, err
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("empty access token")
	}
	return &auth.Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}, nil
}
