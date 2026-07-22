package kiro

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/auth"
)

const socialUserAgent = "kiro-cli/1.0.0"

// buildSocialLoginURL returns the Kiro social-login URL for Google or GitHub.
func buildSocialLoginURL(provider, codeChallenge, state string) string {
	idp := "Google"
	if strings.ToLower(provider) == "github" {
		idp = "Github"
	}
	return fmt.Sprintf(
		"%s/login?idp=%s&redirect_uri=%s&code_challenge=%s&code_challenge_method=S256&state=%s&prompt=select_account",
		authServiceBase,
		idp,
		urlEncode(socialRedirectURI),
		urlEncode(codeChallenge),
		urlEncode(state),
	)
}

// exchangeSocialCode swaps the authorization code for Kiro tokens.
func exchangeSocialCode(ctx context.Context, cli *http.Client, code, codeVerifier string) (string, string, string, int, error) {
	body := map[string]string{
		"code":          code,
		"code_verifier": codeVerifier,
		"redirect_uri":  socialRedirectURI,
	}
	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authServiceBase+"/oauth/token", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", socialUserAgent)

	resp, err := cli.Do(req)
	if err != nil {
		return "", "", "", 0, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", "", 0, fmt.Errorf("social token exchange failed %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ProfileArn   string `json:"profileArn"`
		ExpiresIn    int    `json:"expiresIn"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", "", 0, fmt.Errorf("parse social token response: %w", err)
	}
	if result.AccessToken == "" {
		return "", "", "", 0, fmt.Errorf("social token exchange returned empty access token")
	}
	if result.ExpiresIn <= 0 {
		result.ExpiresIn = 3600
	}
	return result.AccessToken, result.RefreshToken, result.ProfileArn, result.ExpiresIn, nil
}

// refreshSocial calls the Kiro /refreshToken endpoint for social/import tokens.
func refreshSocial(ctx context.Context, cli *http.Client, creds *auth.Credentials) (*auth.Credentials, error) {
	if creds.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}
	body := map[string]string{"refreshToken": creds.RefreshToken}
	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authServiceBase+"/refreshToken", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", socialUserAgent)

	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("social refresh failed %d: %s", resp.StatusCode, string(respBody))
	}

	tok := parseFlexibleToken(respBody)
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("social refresh returned empty access token")
	}

	newCreds := *creds
	newCreds.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		newCreds.RefreshToken = tok.RefreshToken
	}
	if tok.ProfileArn != "" && newCreds.ProviderSpecific != nil {
		newCreds.ProviderSpecific["profileArn"] = tok.ProfileArn
	}
	if tok.ExpiresIn > 0 {
		newCreds.ExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}
	return &newCreds, nil
}
