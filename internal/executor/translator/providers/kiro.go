package providers

import (
	"github.com/rickicode/AxonRouter-Go/internal/executor/translator"
	"github.com/tidwall/gjson"
)

// TranslateKiro converts a Kiro/AWS CodeWhisperer error response into an
// OpenAI-compatible error JSON. Kiro's event-stream errors are already close to
// OpenAI shape, so we map the common fields and fall back to HTTP status mapping
// when the body isn't a recognizable error object.
func TranslateKiro(statusCode int, body []byte) []byte {
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
	errType := gjson.GetBytes(body, "error.type").String()
	if errType == "" {
		errType = gjson.GetBytes(body, "type").String()
	}
	typ, code := translator.MapHTTPStatus(statusCode)
	switch errType {
	case "invalid_request_error":
		typ = "invalid_request_error"
		code = translator.InferCodeFromMessage(msg, statusCode, "bad_request")
	case "authentication_error":
		typ = "authentication_error"
		code = "invalid_api_key"
	case "rate_limit_error":
		typ = "rate_limit_error"
		code = "rate_limit_exceeded"
	case "server_error":
		typ = "server_error"
		code = "internal_server_error"
	}
	return translator.BuildError(msg, typ, code)
}
