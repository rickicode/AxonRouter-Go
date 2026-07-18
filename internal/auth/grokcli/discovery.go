package grokcli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Discover resolves xAI OAuth endpoints through OIDC discovery.
func (s *OAuthService) Discover(ctx context.Context) (*Discovery, error) {
	if s.testDiscoveryResponse != nil {
		return s.testDiscoveryResponse, nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("grokcli discovery: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("grokcli discovery: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("grokcli discovery: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("grokcli discovery failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
		TokenEndpoint               string `json:"token_endpoint"`
	}
	if err = json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("grokcli discovery: parse response: %w", err)
	}

	deviceAuthEndpoint, err := validateEndpoint(payload.DeviceAuthorizationEndpoint, "device_authorization_endpoint")
	if err != nil {
		return nil, err
	}
	tokenEndpoint, err := validateEndpoint(payload.TokenEndpoint, "token_endpoint")
	if err != nil {
		return nil, err
	}

	return &Discovery{
		DeviceAuthorizationEndpoint: deviceAuthEndpoint,
		TokenEndpoint:               tokenEndpoint,
	}, nil
}

func validateEndpoint(rawURL, field string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("grokcli discovery %s is empty", field)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("grokcli discovery %s is invalid: %w", field, err)
	}
	if parsed.Scheme != "https" {
		return "", fmt.Errorf("grokcli discovery %s must use https: %q", field, rawURL)
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host != "x.ai" && !strings.HasSuffix(host, ".x.ai") {
		return "", fmt.Errorf("grokcli discovery %s host %q is not on x.ai", field, host)
	}
	return rawURL, nil
}
