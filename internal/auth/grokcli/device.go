package grokcli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

// requestDeviceCode asks xAI for a device authorization code.
func (s *OAuthService) requestDeviceCode(ctx context.Context, deviceAuthEndpoint, tokenEndpoint string) (*DeviceCodeResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(deviceAuthEndpoint) == "" {
		return nil, fmt.Errorf("grokcli device code: device authorization endpoint is required")
	}

	form := url.Values{
		"client_id": {ClientID},
		"scope":     {Scope},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceAuthEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("grokcli device code: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("grokcli device code request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("grokcli device code: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("grokcli device code request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var deviceCode DeviceCodeResponse
	if err = json.Unmarshal(body, &deviceCode); err != nil {
		return nil, fmt.Errorf("grokcli device code: parse response: %w", err)
	}
	if strings.TrimSpace(deviceCode.DeviceCode) == "" {
		return nil, fmt.Errorf("grokcli device code: response missing device_code")
	}
	if strings.TrimSpace(deviceCode.UserCode) == "" {
		return nil, fmt.Errorf("grokcli device code: response missing user_code")
	}
	if strings.TrimSpace(deviceCode.VerificationURI) == "" && strings.TrimSpace(deviceCode.VerificationURIComplete) == "" {
		return nil, fmt.Errorf("grokcli device code: response missing verification URI")
	}
	if strings.TrimSpace(deviceCode.VerificationURI) == "" {
		deviceCode.VerificationURI = deviceCode.VerificationURIComplete
	}
	deviceCode.TokenEndpoint = strings.TrimSpace(tokenEndpoint)
	return &deviceCode, nil
}

// pollForToken polls the token endpoint until the user authorizes or the device code expires.
func (s *OAuthService) pollForToken(ctx context.Context, deviceCode *DeviceCodeResponse) (*auth.Credentials, error) {
	if deviceCode == nil {
		return nil, fmt.Errorf("grokcli device code: response is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	tokenEndpoint := strings.TrimSpace(deviceCode.TokenEndpoint)
	if tokenEndpoint == "" {
		discovery, err := s.Discover(ctx)
		if err != nil {
			return nil, err
		}
		tokenEndpoint = discovery.TokenEndpoint
	}

	minInterval := defaultPollInterval
	if s.testMinPollInterval > 0 {
		minInterval = s.testMinPollInterval
	}
	interval := time.Duration(deviceCode.Interval) * time.Second
	if interval < minInterval {
		interval = minInterval
	}

	deadline := time.Now().Add(maxPollDuration)
	if deviceCode.ExpiresIn > 0 {
		codeDeadline := time.Now().Add(time.Duration(deviceCode.ExpiresIn) * time.Second)
		if codeDeadline.Before(deadline) {
			deadline = codeDeadline
		}
	}

	firstAttempt := true
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("grokcli device code: context cancelled: %w", ctx.Err())
		case <-timer.C:
			if !firstAttempt && time.Now().After(deadline) {
				return nil, fmt.Errorf("grokcli device code expired")
			}
			firstAttempt = false

			creds, pollErr, nextInterval, shouldContinue := s.exchangeDeviceCode(ctx, tokenEndpoint, deviceCode.DeviceCode, interval)
			if creds != nil {
				return creds, nil
			}
			if !shouldContinue {
				return nil, pollErr
			}
			interval = nextInterval
			timer.Reset(interval)
		}
	}
}

// exchangeDeviceCode attempts to exchange a device code for tokens.
func (s *OAuthService) exchangeDeviceCode(ctx context.Context, tokenEndpoint, deviceCode string, interval time.Duration) (*auth.Credentials, error, time.Duration, bool) {
	form := url.Values{
		"grant_type":  {DeviceCodeGrantType},
		"device_code": {strings.TrimSpace(deviceCode)},
		"client_id":   {ClientID},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(tokenEndpoint), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("grokcli device token: create request: %w", err), interval, false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("grokcli device token request failed: %w", err), interval, false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("grokcli device token: read response: %w", err), interval, false
	}

	var payload struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		AccessToken      string `json:"access_token"`
		RefreshToken     string `json:"refresh_token"`
		IDToken          string `json:"id_token"`
		TokenType        string `json:"token_type"`
		ExpiresIn        int    `json:"expires_in"`
	}
	if err = json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("grokcli device token: parse response: %w", err), interval, false
	}

	if payload.Error != "" {
		switch payload.Error {
		case "authorization_pending":
			return nil, nil, interval, true
		case "slow_down":
			slowStep := defaultPollInterval
			if s.testMinPollInterval > 0 {
				slowStep = s.testMinPollInterval
			}
			nextInterval := interval + slowStep
			return nil, nil, nextInterval, true
		case "expired_token":
			return nil, fmt.Errorf("grokcli device code expired"), interval, false
		case "access_denied":
			return nil, fmt.Errorf("grokcli device authorization denied"), interval, false
		default:
			desc := strings.TrimSpace(payload.ErrorDescription)
			if desc != "" {
				return nil, fmt.Errorf("grokcli device token error: %s: %s", payload.Error, desc), interval, false
			}
			return nil, fmt.Errorf("grokcli device token error: %s", payload.Error), interval, false
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("grokcli device token request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body))), interval, false
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return nil, fmt.Errorf("grokcli device token response missing access_token"), interval, false
	}

	email, sub := parseJWTIdentity(payload.IDToken)
	return buildCredentials(payload.AccessToken, payload.RefreshToken, payload.IDToken, payload.TokenType, tokenEndpoint, payload.ExpiresIn, email, sub), nil, interval, false
}

func buildCredentials(accessToken, refreshToken, idToken, tokenType, tokenEndpoint string, expiresIn int, email, sub string) *auth.Credentials {
	creds := &auth.Credentials{
		AccessToken:  strings.TrimSpace(accessToken),
		RefreshToken: strings.TrimSpace(refreshToken),
		IDToken:      strings.TrimSpace(idToken),
		Email:        email,
		AccountID:    sub,
		ProviderSpecific: map[string]string{
			"token_endpoint": tokenEndpoint,
			"email":          email,
			"sub":            sub,
		},
	}
	if strings.TrimSpace(tokenType) != "" {
		creds.ProviderSpecific["token_type"] = strings.TrimSpace(tokenType)
	}
	if expiresIn > 0 {
		creds.ExpiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	}
	return creds
}

// parseJWTIdentity extracts email and subject from a JWT ID token.
func parseJWTIdentity(token string) (email, sub string) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return "", ""
	}
	payload := parts[1]
	payload += strings.Repeat("=", (4-len(payload)%4)%4)
	raw, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return "", ""
	}
	var claims map[string]any
	if err = json.Unmarshal(raw, &claims); err != nil {
		return "", ""
	}
	if v, ok := claims["email"].(string); ok {
		email = strings.TrimSpace(v)
	}
	if v, ok := claims["sub"].(string); ok {
		sub = strings.TrimSpace(v)
	}
	return email, sub
}
