package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

// DeviceCodeEndpoints are the OAuth device-code endpoints for OpenAI.
const (
	DeviceCodeURL = "https://auth.openai.com/oauth/device/code"
)

// DeviceFlow represents an in-progress device authorization grant.
type DeviceFlow struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	CompleteURI     string
	ExpiresIn       int
	Interval        int
}

// StartDeviceFlow requests a device code from the OpenAI authorization server.
func (s *OAuthService) StartDeviceFlow(ctx context.Context) (*DeviceFlow, error) {
	form := url.Values{}
	form.Set("client_id", ClientID)
	form.Set("scope", "openid profile email offline_access")

	req, err := http.NewRequestWithContext(ctx, "POST", s.deviceURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create device-code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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
		return nil, fmt.Errorf("device-code failed %d: %s", resp.StatusCode, string(body))
	}

	var payload struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse device-code response: %w", err)
	}
	if payload.Interval <= 0 {
		payload.Interval = 5
	}
	if payload.ExpiresIn <= 0 {
		payload.ExpiresIn = 600
	}
	return &DeviceFlow{
		DeviceCode:      payload.DeviceCode,
		UserCode:        payload.UserCode,
		VerificationURI: payload.VerificationURI,
		CompleteURI:     payload.VerificationURIComplete,
		ExpiresIn:       payload.ExpiresIn,
		Interval:        payload.Interval,
	}, nil
}

// PollDeviceFlow periodically exchanges the device code for tokens.
// It returns either credentials or an error. interval and expiry are taken from
// the DeviceFlow response; override values may be passed for tests.
func (s *OAuthService) PollDeviceFlow(ctx context.Context, flow *DeviceFlow) (*auth.Credentials, error) {
	if flow == nil {
		return nil, fmt.Errorf("nil device flow")
	}
	interval := time.Duration(flow.Interval) * time.Second
	if interval < time.Second {
		interval = time.Second
	}
	deadline := time.Now().Add(time.Duration(flow.ExpiresIn) * time.Second)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("device-code expired")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}

		form := url.Values{}
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		form.Set("client_id", ClientID)
		form.Set("device_code", flow.DeviceCode)

		req, err := http.NewRequestWithContext(ctx, "POST", s.tokenURL, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, fmt.Errorf("create token request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("token request: %w", err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read token response: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			return s.parseTokenResponse(body)
		}

		// Handle OAuth 2.0 device-flow errors.
		var oerr struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &oerr)
		switch oerr.Error {
		case "authorization_pending", "slow_down":
			if oerr.Error == "slow_down" {
				interval += 5 * time.Second
			}
			continue
		case "expired_token":
			return nil, fmt.Errorf("device-code expired")
		case "access_denied":
			return nil, fmt.Errorf("device-code denied")
		default:
			return nil, fmt.Errorf("token exchange failed %d: %s", resp.StatusCode, string(body))
		}
	}
}

func (s *OAuthService) parseTokenResponse(body []byte) (*auth.Credentials, error) {
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
	accountID, email := extractTokenClaims(tokenResp.IDToken)
	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	return &auth.Credentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		AccountID:    accountID,
		Email:        email,
		ExpiresAt:    expiresAt,
	}, nil
}
