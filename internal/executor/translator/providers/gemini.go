package providers

import (
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/executor/translator"
	"github.com/tidwall/gjson"
)

// TranslateGemini converts a Google Gemini error response into an
// OpenAI-compatible error JSON.
//
// Gemini format:
//
//	{"error":{"code":400,"message":"...","status":"INVALID_ARGUMENT"}}
func TranslateGemini(statusCode int, body []byte) []byte {
	if !gjson.ValidBytes(body) {
		typ, code := translator.MapHTTPStatus(statusCode)
		return translator.BuildError(string(body), typ, code)
	}

	msg := gjson.GetBytes(body, "error.message").String()
	if msg == "" {
		msg = gjson.GetBytes(body, "message").String()
	}
	if msg == "" {
		msg = string(body)
	}

	typ, code := translator.MapHTTPStatus(statusCode)

	// Gemini uses a status string inside error.status (e.g. INVALID_ARGUMENT).
	switch strings.ToUpper(gjson.GetBytes(body, "error.status").String()) {
	case "INVALID_ARGUMENT", "FAILED_PRECONDITION":
		typ = "invalid_request_error"
		code = translator.InferCodeFromMessage(msg, statusCode, "bad_request")
	case "UNAUTHENTICATED":
		typ = "authentication_error"
		code = "invalid_api_key"
	case "PERMISSION_DENIED":
		typ = "permission_error"
		code = "insufficient_quota"
	case "NOT_FOUND":
		typ = "invalid_request_error"
		code = "model_not_found"
	case "RESOURCE_EXHAUSTED":
		typ = "rate_limit_error"
		code = "rate_limit_exceeded"
	case "INTERNAL", "UNAVAILABLE", "DEADLINE_EXCEEDED":
		typ = "server_error"
		code = "internal_server_error"
	}

	return translator.BuildError(msg, typ, code)
}
