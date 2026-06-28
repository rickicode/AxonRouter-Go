package connstate

import (
	"net/http"
	"strconv"
)

// ParseRateLimitHeaders extracts rate limit information from response headers
// and updates the ModelLimitState for the given connection and model.
//
// Supports OpenAI-style headers:
//   - x-ratelimit-remaining-requests → RPM remaining
//   - x-ratelimit-remaining-tokens → TPM remaining
//
// And Claude-style headers:
//   - anthropic-ratelimit-requests-remaining → RPM remaining
//   - anthropic-ratelimit-tokens-remaining → TPM remaining
func ParseRateLimitHeaders(headers http.Header, store *Store, connID string, modelID string) {
	if headers == nil || store == nil || connID == "" || modelID == "" {
		return
	}

	cs := store.Get(connID)
	if cs == nil {
		return
	}

	mls := cs.GetModelLimit(modelID)

	// OpenAI-style
	if v := headers.Get("x-ratelimit-remaining-requests"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			mls.SetRPMRemaining(n)
		}
	}
	if v := headers.Get("x-ratelimit-remaining-tokens"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			mls.SetTPMRemaining(n)
		}
	}

	// Claude-style
	if v := headers.Get("anthropic-ratelimit-requests-remaining"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			mls.SetRPMRemaining(n)
		}
	}
	if v := headers.Get("anthropic-ratelimit-tokens-remaining"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			mls.SetTPMRemaining(n)
		}
	}
}
