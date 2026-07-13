package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

// Codex device-flow endpoints as used by the official Codex CLI.
const (
	DeviceUserCodeURL         = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	DeviceTokenURL            = "https://auth.openai.com/api/accounts/deviceauth/token"
	DeviceVerificationURL     = "https://auth.openai.com/codex/device"
	DeviceExchangeRedirectURI = "https://auth.openai.com/deviceauth/callback"
	DeviceDefaultTimeout      = 15 * time.Minute
	DeviceDefaultPollInterval = 5 * time.Second
)

// DeviceUserCodeResponse is returned by the user-code endpoint.
type DeviceUserCodeResponse struct {
	DeviceAuthID string          `json:"device_auth_id"`
	UserCode     string          `json:"user_code"`
	UserCodeAlt  string          `json:"usercode"`
	Interval     json.RawMessage `json:"interval"`
}

// DeviceTokenResponse is returned by the token endpoint once the user approves.
type DeviceTokenResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
	CodeChallenge     string `json:"code_challenge"`
}

// RequestDeviceUserCode asks the OpenAI authorization server for a device code.
func (s *OAuthService) RequestDeviceUserCode(ctx context.Context) (*DeviceUserCodeResponse, error) {
	body, err := json.Marshal(map[string]string{"client_id": ClientID})
	if err != nil {
		return nil, fmt.Errorf("marshal device user-code request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.deviceUserCodeURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create device user-code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device user-code request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read device user-code response: %w", err)
	}
	if !isDeviceSuccessStatus(resp.StatusCode) {
		trimmed := strings.TrimSpace(string(respBody))
		if trimmed == "" {
			trimmed = "empty response body"
		}
		return nil, fmt.Errorf("device user-code failed %d: %s", resp.StatusCode, trimmed)
	}

	var parsed DeviceUserCodeResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("parse device user-code response: %w", err)
	}
	if parsed.UserCode == "" {
		parsed.UserCode = strings.TrimSpace(parsed.UserCodeAlt)
	}
	if parsed.DeviceAuthID == "" || parsed.UserCode == "" {
		return nil, fmt.Errorf("device user-code response missing required fields")
	}
	return &parsed, nil
}

// PollDeviceToken polls the device token endpoint until the user approves or
// the deadline is reached. 403/404 are treated as "authorization pending".
func (s *OAuthService) PollDeviceToken(ctx context.Context, deviceAuthID, userCode string, interval, timeout time.Duration) (*DeviceTokenResponse, error) {
	if timeout <= 0 {
		timeout = DeviceDefaultTimeout
	}
	if interval <= 0 {
		interval = DeviceDefaultPollInterval
	}
	deadline := time.Now().Add(timeout)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("device authentication timed out after %v", timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}

		body, err := json.Marshal(map[string]string{
			"device_auth_id": deviceAuthID,
			"user_code":      userCode,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal device token request: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.deviceTokenURL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create device token request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("device token request: %w", err)
		}
		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read device token response: %w", readErr)
		}

		switch {
		case isDeviceSuccessStatus(resp.StatusCode):
			var parsed DeviceTokenResponse
			if err := json.Unmarshal(respBody, &parsed); err != nil {
				return nil, fmt.Errorf("parse device token response: %w", err)
			}
			if strings.TrimSpace(parsed.AuthorizationCode) == "" || strings.TrimSpace(parsed.CodeVerifier) == "" {
				return nil, fmt.Errorf("device token response missing authorization_code or code_verifier")
			}
			return &parsed, nil
		case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound:
			// Authorization still pending; keep polling.
			continue
		default:
			trimmed := strings.TrimSpace(string(respBody))
			if trimmed == "" {
				trimmed = "empty response body"
			}
			return nil, fmt.Errorf("device token polling failed %d: %s", resp.StatusCode, trimmed)
		}
	}
}

// RunDeviceFlow performs the full device-code flow and returns credentials.
func (s *OAuthService) RunDeviceFlow(ctx context.Context) (*auth.Credentials, string, error) {
	userCodeResp, err := s.RequestDeviceUserCode(ctx)
	if err != nil {
		return nil, "", err
	}
	interval := parseDevicePollInterval(userCodeResp.Interval)
	tokenResp, err := s.PollDeviceToken(ctx, userCodeResp.DeviceAuthID, userCodeResp.UserCode, interval, DeviceDefaultTimeout)
	if err != nil {
		return nil, "", err
	}
	creds, err := s.exchangeCode(ctx, tokenResp.AuthorizationCode, DeviceExchangeRedirectURI, tokenResp.CodeVerifier)
	if err != nil {
		return nil, "", fmt.Errorf("exchange device authorization code: %w", err)
	}
	return creds, userCodeResp.UserCode, nil
}

func parseDevicePollInterval(raw json.RawMessage) time.Duration {
	if len(raw) == 0 {
		return DeviceDefaultPollInterval
	}
	var asStr string
	if err := json.Unmarshal(raw, &asStr); err == nil {
		if sec, convErr := strconv.Atoi(strings.TrimSpace(asStr)); convErr == nil && sec > 0 {
			return time.Duration(sec) * time.Second
		}
	}
	var asInt int
	if err := json.Unmarshal(raw, &asInt); err == nil && asInt > 0 {
		return time.Duration(asInt) * time.Second
	}
	return DeviceDefaultPollInterval
}

func isDeviceSuccessStatus(code int) bool { return code >= 200 && code < 300 }
