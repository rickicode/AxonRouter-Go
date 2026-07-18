package providers

import (
	"encoding/json"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/executor/translator"
	"github.com/tidwall/gjson"
)

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
	lowerMsg := strings.ToLower(msg)

	switch statusCode {
	case 402:
		typ = "rate_limit_error"
		code = "insufficient_quota"
	case 401:
		typ = "authentication_error"
		code = "invalid_api_key"
	case 403:
		typ = "permission_error"
		code = "insufficient_quota"
	case 429:
		typ = "rate_limit_error"
		code = "rate_limit_exceeded"
	}

	switch {
	case strings.Contains(lowerMsg, "spending limit") ||
		strings.Contains(lowerMsg, "spending-limit") ||
		strings.Contains(lowerMsg, "quota") ||
		strings.Contains(lowerMsg, "limit reached"):
		typ = "rate_limit_error"
		code = "insufficient_quota"
	case strings.Contains(lowerMsg, "maximum context length") ||
		strings.Contains(lowerMsg, "context length") ||
		strings.Contains(lowerMsg, "context_length_exceeded") ||
		strings.Contains(lowerMsg, "context_too_large") ||
		strings.Contains(lowerMsg, "too many tokens"):
		typ = "invalid_request_error"
		code = "context_length_exceeded"
	case strings.Contains(lowerMsg, "invalid api key") ||
		strings.Contains(lowerMsg, "unauthorized") ||
		strings.Contains(lowerMsg, "authentication"):
		if statusCode >= 400 && statusCode < 500 {
			typ = "authentication_error"
			code = "invalid_api_key"
		}
	}

	errResp := translator.OpenAIError{}
	errResp.Error.Message = msg
	errResp.Error.Type = typ
	errResp.Error.Code = code
	b, _ := json.Marshal(errResp)
	return b
}

func grokcliFallback(statusCode int, text string) []byte {
	typ, code := translator.MapHTTPStatus(statusCode)
	return translator.BuildError(text, typ, code)
}
