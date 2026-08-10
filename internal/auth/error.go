package auth

import "strings"

// IsAuthError reports whether an error indicates an authentication/authorization
// failure (HTTP 401/403 or equivalent). It is used by request handlers and by
// background workers to decide whether to attempt a token refresh and retry.
func IsAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "401") || strings.Contains(msg, "403") ||
		strings.Contains(msg, "unauthorized") || strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "authentication") || strings.Contains(msg, "access denied") ||
		strings.Contains(msg, "bad credentials")
}
