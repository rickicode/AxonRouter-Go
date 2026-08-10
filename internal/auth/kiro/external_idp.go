package kiro

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
)

// allowedExternalIDPHostSuffixes constrains where external IdP tokens may be
// refreshed. Values beginning with "." are treated as host suffixes.
var allowedExternalIDPHostSuffixes = []string{
	"login.microsoftonline.com",
	"login.microsoftonline.us",
	"login.partner.microsoftonline.cn",
	"login.microsoft.com",
	"login.windows.net",
	"sts.windows.net",
	".okta.com",
	".oktapreview.com",
	".okta-emea.com",
	".auth0.com",
	".onelogin.com",
	".pingidentity.com",
	".pingone.com",
	"accounts.google.com",
	"oauth2.googleapis.com",
	".amazoncognito.com",
}

// validateExternalIdpTokenEndpoint ensures tokenEndpoint uses HTTPS and points to
// an allowed enterprise IdP host.
func validateExternalIdpTokenEndpoint(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("tokenEndpoint is required for external_idp")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("tokenEndpoint must be a valid URL")
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("tokenEndpoint must use https")
	}
	host := strings.ToLower(u.Hostname())
	allowed := false
	for _, suffix := range allowedExternalIDPHostSuffixes {
		if strings.HasPrefix(suffix, ".") {
			if strings.HasSuffix(host, suffix) {
				allowed = true
				break
			}
		} else if host == suffix {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("tokenEndpoint host is not an allowed identity provider: %s", host)
	}
	return u.String(), nil
}

// normalizeScope turns a string or array-like value into a space-delimited scope.
func normalizeScope(scope string) string {
	return strings.Join(strings.Fields(scope), " ")
}

// refreshExternalIdpToken performs a public-client OAuth2 refresh_token grant
// against an enterprise IdP token endpoint.
func refreshExternalIdpToken(ctx context.Context, cli *http.Client, refreshToken string, psd map[string]string) (string, string, time.Time, error) {
	if refreshToken == "" {
		return "", "", time.Time{}, fmt.Errorf("refresh token is required for external_idp")
	}
	clientID := strings.TrimSpace(psd["clientId"])
	if clientID == "" {
		return "", "", time.Time{}, fmt.Errorf("clientId is required for external_idp refresh")
	}
	tokenEndpoint := strings.TrimSpace(psd["tokenEndpoint"])
	if _, err := validateExternalIdpTokenEndpoint(tokenEndpoint); err != nil {
		return "", "", time.Time{}, err
	}
	scope := normalizeScope(psd["scope"])
	if scope == "" {
		return "", "", time.Time{}, fmt.Errorf("scope is required for external_idp refresh")
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
		"scope":         {scope},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := cli.Do(req)
	if err != nil {
		return "", "", time.Time{}, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", time.Time{}, fmt.Errorf("external IdP refresh failed %d: %s", resp.StatusCode, string(body))
	}

	tok := parseFlexibleToken(body)
	if tok.AccessToken == "" {
		return "", "", time.Time{}, fmt.Errorf("external IdP refresh returned empty access token")
	}
	expiresAt := time.Time{}
	if tok.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}
	return tok.AccessToken, tok.RefreshToken, expiresAt, nil
}

// emailFromExternalIdpToken extracts a human-readable identity claim from an
// enterprise IdP JWT.
func emailFromExternalIdpToken(accessToken string) string {
	claims := decodeJWTPayload(accessToken)
	if claims == nil {
		return ""
	}
	for _, key := range []string{"email", "preferred_username", "upn"} {
		if v, ok := claims[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// decodeJWTPayload returns the payload claims of a JWT without verifying the signature.
func decodeJWTPayload(jwt string) map[string]any {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return nil
	}
	payload := strings.NewReplacer("-", "+", "_", "/").Replace(parts[1])
	padding := (4 - len(payload)%4) % 4
	payload += strings.Repeat("=", padding)
	b, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil
	}
	var claims map[string]any
	if err := json.Unmarshal(b, &claims); err != nil {
		return nil
	}
	return claims
}
