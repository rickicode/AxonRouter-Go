package kiro

import (
	"strings"
)

// isAWSRefreshToken returns true for AWS SSO OIDC refresh tokens that can be
// refreshed through the Kiro/AWS token endpoints.
func isAWSRefreshToken(token string) bool {
	return strings.HasPrefix(strings.TrimSpace(token), "aorAAAAAG")
}
