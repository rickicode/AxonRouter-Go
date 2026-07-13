package providers

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/executor/translator"
	"github.com/tidwall/gjson"
)

// TranslateCodex converts a Codex upstream error body into an OpenAI-compatible
// error JSON. Codex errors look like:
//
//	{"error":{"type":"usage_limit_reached","message":"...","resets_at":1234567890,"resets_in_seconds":3600}}
//
// It maps usage_limit_reached to a 429 rate_limit with Retry-After, auth
// failures to 401, and context-length errors to a non-retryable
// invalid_request_error.
func TranslateCodex(statusCode int, body []byte) []byte {
	if !gjson.ValidBytes(body) {
		return codexFallback(statusCode, string(body))
	}

	errObj := gjson.GetBytes(body, "error")
	if !errObj.Exists() {
		errObj = gjson.ParseBytes(body)
	}

	msg := errObj.Get("message").String()
	if msg == "" {
		msg = string(body)
	}

	typ, code := translator.MapHTTPStatus(statusCode)
	errType := errObj.Get("type").String()
	lowerType := strings.ToLower(errType)
	lowerMsg := strings.ToLower(msg)

	var retryAfter *int64
	switch lowerType {
	case "usage_limit_reached":
		typ = "rate_limit_error"
		code = "insufficient_quota"
		if ra := codexRetryAfter(errObj); !ra.IsZero() {
			secs := int64(ra.Sub(time.Now()).Seconds())
			if secs > 0 {
				retryAfter = &secs
			}
		}
	case "invalid_request_error":
		typ = "invalid_request_error"
		code = translator.InferCodeFromMessage(msg, statusCode, "bad_request")
	case "authentication_error":
		typ = "authentication_error"
		code = "invalid_api_key"
	case "permission_error":
		typ = "permission_error"
		code = "insufficient_quota"
	case "rate_limit_error":
		typ = "rate_limit_error"
		code = "rate_limit_exceeded"
	case "server_error":
		typ = "server_error"
		code = "internal_server_error"
	}

	// Body-text overrides for cases where the type field is generic.
	switch {
	case strings.Contains(lowerMsg, "selected model is at capacity"):
		typ = "rate_limit_error"
		code = "rate_limit_exceeded"
	case strings.Contains(lowerMsg, "context length") ||
		strings.Contains(lowerMsg, "context_length_exceeded") ||
		strings.Contains(lowerMsg, "context_too_large") ||
		strings.Contains(lowerMsg, "maximum context length"):
		typ = "invalid_request_error"
		code = "context_length_exceeded"
	case strings.Contains(lowerMsg, "previous_response_not_found") ||
		strings.Contains(lowerMsg, "invalid_encrypted_content"):
		typ = "invalid_request_error"
		code = "bad_request"
	case strings.Contains(lowerMsg, "invalid or expired token") ||
		strings.Contains(lowerMsg, "refresh_token_reused"):
		if statusCode >= 400 && statusCode < 500 {
			typ = "authentication_error"
			code = "invalid_api_key"
		}
	}

	errResp := translator.OpenAIError{}
	errResp.Error.Message = msg
	errResp.Error.Type = typ
	errResp.Error.Code = code
	if retryAfter != nil {
		errResp.Error.RetryAfter = retryAfter
	}
	b, _ := json.Marshal(errResp)
	return b
}

// codexRetryAfter returns a future time derived from resets_at or
// resets_in_seconds when present. It returns the zero value if neither is set.
func codexRetryAfter(errObj gjson.Result) time.Time {
	if v := errObj.Get("resets_at"); v.Exists() {
		n := v.Int()
		if n > 0 {
			return time.Unix(n, 0)
		}
	}
	if v := errObj.Get("resets_in_seconds"); v.Exists() {
		n := v.Int()
		if n > 0 {
			return time.Now().Add(time.Duration(n) * time.Second)
		}
	}
	if v := errObj.Get("retry_after"); v.Exists() {
		n := v.Int()
		if n > 0 {
			return time.Now().Add(time.Duration(n) * time.Second)
		}
		if s := v.String(); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				return time.Now().Add(time.Duration(n) * time.Second)
			}
		}
	}
	return time.Time{}
}

func codexFallback(statusCode int, text string) []byte {
	typ, code := translator.MapHTTPStatus(statusCode)
	return translator.BuildError(text, typ, code)
}
