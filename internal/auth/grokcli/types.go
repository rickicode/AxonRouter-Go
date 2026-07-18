// Package grokcli provides OAuth2 device-flow authentication for the Grok CLI provider.
package grokcli

import "time"

const (
	// DiscoveryURL is xAI's OIDC discovery endpoint.
	DiscoveryURL = "https://auth.x.ai/.well-known/openid-configuration"
	// Issuer is xAI's OAuth issuer.
	Issuer = "https://auth.x.ai"
	// ClientID is the public Grok CLI OAuth client ID.
	ClientID = "b1a00492-073a-47ea-816f-4c329264a828"
	// Scope is the OAuth scope set required for Grok CLI access.
	Scope = "openid profile email offline_access grok-cli:access api:access"
	// DeviceCodeGrantType is the OAuth2 device authorization grant type (RFC 8628).
	DeviceCodeGrantType = "urn:ietf:params:oauth:grant-type:device_code"

	defaultPollInterval = 5 * time.Second
	maxPollDuration     = 30 * time.Minute
	httpClientTimeout   = 30 * time.Second
)

// Discovery holds OAuth endpoints resolved from xAI OIDC discovery.
type Discovery struct {
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
}

// DeviceCodeResponse represents xAI's device authorization response.
type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
	TokenEndpoint           string `json:"-"`
}
