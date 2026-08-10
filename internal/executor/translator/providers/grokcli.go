package providers

import (
	"encoding/json"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/executor/translator"
	"github.com/tidwall/gjson"
)

// grokNestedError holds a Grok error object that is sometimes embedded as a
// JSON string inside error.message (e.g. double-encoded payloads from the
// x.ai chat endpoint).
type grokNestedError struct {
	Message string `json:"error"`
	AltMsg  string `json:"message"`
	Code    string `json:"code"`
	Type    string `json:"type"`
}

func (e grokNestedError) text() string {
	if e.Message != "" {
		return e.Message
	}
	return e.AltMsg
}

// decodeGrokNestedMessage attempts to parse a string that is itself a JSON
// error object. Grok CLI occasionally returns error.message as a serialized
// JSON string; decoding it lets us see the real semantic code (e.g.
// "permission-denied") and avoids mis-classifying the error as quota just
// because the embedded object contains "insufficient_quota".
func decodeGrokNestedMessage(msg string) grokNestedError {
	s := strings.TrimSpace(msg)
	if s == "" || (s[0] != '{' && s[0] != '[') {
		return grokNestedError{}
	}
	var inner grokNestedError
	if err := json.Unmarshal([]byte(s), &inner); err != nil {
		return grokNestedError{}
	}
	return inner
}

// TranslateGrokCLI converts a Grok CLI upstream error body into an
// OpenAI-compatible error JSON. It maps spending-limit/quota failures to
// 429 insufficient_quota, auth failures to 401/403 errors, context-length
// errors, and generic 429s.
func TranslateGrokCLI(statusCode int, body []byte) []byte {
	if !gjson.ValidBytes(body) {
		return grokcliFallback(statusCode, string(body))
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

	// Grok sometimes double-encodes the real error object inside
	// error.message. Decode it so we can inspect the real semantic code.
	inner := decodeGrokNestedMessage(msg)
	search := strings.ToLower(msg + " " + inner.text() + " " + inner.Code + " " + inner.Type)

	// Permission/auth errors take precedence. A response with "permission-denied"
	// or "access to the chat endpoint is denied" must never be reported as a
	// rate-limit/quota error just because the body also mentions "quota".
	// Generic 403s without these signals keep the default permission_error/
	// insufficient_quota mapping so existing tests/clients stay stable.
	isPermissionDenied := strings.Contains(search, "permission-denied") ||
		strings.Contains(search, "permission denied") ||
		strings.Contains(search, "permission_error") ||
		strings.Contains(search, "access to the chat endpoint is denied") ||
		strings.Contains(search, "access denied") ||
		strings.Contains(search, "incorrect credentials")

	if isPermissionDenied && statusCode >= 400 && statusCode < 500 {
		typ = "permission_error"
		code = "permission_error"
		if strings.Contains(search, "invalid api key") ||
			strings.Contains(search, "unauthorized") ||
			strings.Contains(search, "authentication") {
			typ = "authentication_error"
			code = "invalid_api_key"
		}
	} else if strings.Contains(search, "spending limit") ||
		strings.Contains(search, "spending-limit") ||
		strings.Contains(search, "quota") ||
		strings.Contains(search, "limit reached") {
		typ = "rate_limit_error"
		code = "insufficient_quota"
	} else if strings.Contains(search, "maximum context length") ||
		strings.Contains(search, "context length") ||
		strings.Contains(search, "context_length_exceeded") ||
		strings.Contains(search, "context_too_large") ||
		strings.Contains(search, "too many tokens") {
		typ = "invalid_request_error"
		code = "context_length_exceeded"
	} else if strings.Contains(search, "invalid api key") ||
		strings.Contains(search, "unauthorized") ||
		strings.Contains(search, "authentication") {
		if statusCode >= 400 && statusCode < 500 {
			typ = "authentication_error"
			code = "invalid_api_key"
		}
	}

	// Preserve the rich nested message when available so dashboards/logs show
	// the real upstream text, not the double-encoded JSON string.
	outMsg := msg
	if inner.text() != "" {
		outMsg = inner.text()
	}

	errResp := translator.OpenAIError{}
	errResp.Error.Message = outMsg
	errResp.Error.Type = typ
	errResp.Error.Code = code
	b, _ := json.Marshal(errResp)
	return b
}

func grokcliFallback(statusCode int, text string) []byte {
	typ, code := translator.MapHTTPStatus(statusCode)
	return translator.BuildError(text, typ, code)
}
