package kiro

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

// codeWhispererHost returns the regional CodeWhisperer control-plane host.
func codeWhispererHost(region string) string {
	region = strings.ToLower(strings.TrimSpace(region))
	if region == "" || region == "us-east-1" {
		return "https://codewhisperer.us-east-1.amazonaws.com"
	}
	return fmt.Sprintf("https://q.%s.amazonaws.com", region)
}

// listAvailableProfiles calls CodeWhisperer to discover the default profile ARN.
func listAvailableProfiles(ctx context.Context, cli *http.Client, accessToken, region string) (string, error) {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return "", fmt.Errorf("access token is required")
	}
	endpoint := codeWhispererHost(region)
	body := map[string]any{"maxResults": 10}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("x-amz-target", "AmazonCodeWhispererService.ListAvailableProfiles")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := cli.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("list profiles failed %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Profiles []struct {
			Arn        string `json:"arn"`
			ProfileArn string `json:"profileArn"`
			Region     string `json:"region"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse profiles response: %w", err)
	}
	candidate := region
	if candidate == "" {
		candidate = defaultRegion
	}
	for _, p := range result.Profiles {
		arn := firstNonEmpty(p.Arn, p.ProfileArn)
		if arn == "" {
			continue
		}
		if strings.Contains(arn, ":"+candidate+":") {
			return arn, nil
		}
	}
	for _, p := range result.Profiles {
		if arn := firstNonEmpty(p.Arn, p.ProfileArn); arn != "" {
			return arn, nil
		}
	}
	return "", fmt.Errorf("no profiles available")
}

// validateAPIKey proves an API key by listing profiles and returns credentials.
func validateAPIKey(ctx context.Context, cli *http.Client, apiKey, region string) (*auth.Credentials, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required")
	}
	region = strings.ToLower(strings.TrimSpace(region))
	if region == "" {
		region = defaultRegion
	}
	profileArn, err := listAvailableProfiles(ctx, cli, apiKey, region)
	if err != nil {
		return nil, fmt.Errorf("API key validation failed: %w", err)
	}
	return &auth.Credentials{
		AccessToken: apiKey,
		ProviderSpecific: map[string]string{
			"authMethod": "api_key",
			"profileArn": profileArn,
			"region":     region,
		},
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
