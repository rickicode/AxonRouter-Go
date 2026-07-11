package providers

import (
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/executor/translator"
	"github.com/tidwall/gjson"
)

// TranslateClaude converts an Anthropic-style error response into an
// OpenAI-compatible error JSON.
//
// Anthropic format:
//
//	{"type":"error","error":{"type":"invalid_request_error","message":"..."},"request_id":"..."}
func TranslateClaude(statusCode int, body []byte) []byte {
	if !gjson.ValidBytes(body) {
		return fallback(statusCode, string(body))
	}

	msg := gjson.GetBytes(body, "error.message").String()
	if msg == "" {
		msg = gjson.GetBytes(body, "message").String()
	}
	if msg == "" {
		msg = string(body)
	}

	claudeType := gjson.GetBytes(body, "error.type").String()
	if claudeType == "" {
		claudeType = gjson.GetBytes(body, "type").String()
	}

	typ, code := translator.MapHTTPStatus(statusCode)

	switch strings.ToLower(claudeType) {
	case "invalid_request_error":
		typ = "invalid_request_error"
		code = translator.InferCodeFromMessage(msg, statusCode, "bad_request")
	case "authentication_error":
		typ = "authentication_error"
		code = "invalid_api_key"
	case "billing_error":
		typ = "billing_error"
		code = "payment_required"
	case "permission_error":
		typ = "permission_error"
		code = "insufficient_quota"
	case "not_found_error":
		typ = "invalid_request_error"
		code = "model_not_found"
	case "rate_limit_error":
		typ = "rate_limit_error"
		code = "rate_limit_exceeded"
	case "overloaded_error", "timeout_error", "api_error":
		typ = "server_error"
		code = "internal_server_error"
	}

	return translator.BuildError(msg, typ, code)
}

func fallback(statusCode int, text string) []byte {
	typ, code := translator.MapHTTPStatus(statusCode)
	return translator.BuildError(text, typ, code)
}
