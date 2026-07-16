package providers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/executor/translator"
	"github.com/tidwall/gjson"
)

// canonicalOpenAICodes are error.code values that OpenAI-compatible clients
// already understand. If an upstream already uses one of these, keep it.
var canonicalOpenAICodes = map[string]bool{
	"context_length_exceeded": true,
	"rate_limit_exceeded":     true,
	"insufficient_quota":      true,
	"model_not_found":          true,
	"model_not_supported":      true,
	"invalid_api_key":          true,
	"content_filter":           true,
	"bad_request":              true,
	"payment_required":         true,
	"permission_error":         true,
	"not_found_error":          true,
	"invalid_request_error":    true,
	"authentication_error":     true,
	"rate_limit_error":         true,
	"billing_error":            true,
	"server_error":             true,
	"internal_server_error":    true,
	"bad_gateway":              true,
	"service_unavailable":      true,
	"gateway_timeout":          true,
}

// TranslateOpenAICompatible normalizes an upstream error body into a clean
// OpenAI-compatible error envelope. It is intended as the default translator for
// all providers that already speak an OpenAI-style protocol (Bedrock, OpenAI,
// OpenCode Free, Groq, DeepSeek, etc.).
//
// It recognizes both canonical OpenAI codes and provider-specific synonyms such
// as Bedrock's "validation_error", mapping them to the canonical code by
// inspecting the error message.
func TranslateOpenAICompatible(statusCode int, raw []byte) []byte {
	message := string(raw)
	errType, code := translator.MapHTTPStatus(statusCode)
	var param *string

	if gjson.ValidBytes(raw) {
		errObj := gjson.GetBytes(raw, "error")
		if !errObj.Exists() {
			errObj = gjson.ParseBytes(raw)
		}

		if v := errObj.Get("message").String(); v != "" {
			message = v
		}
		if v := errObj.Get("type").String(); v != "" {
			errType = mapOpenAIErrorType(v)
		}
		if v := errObj.Get("code").String(); v != "" {
			if canonicalOpenAICodes[strings.ToLower(v)] {
				code = v
			} else {
				code = translator.InferCodeFromMessage(message+" "+v, statusCode, code)
			}
		}
		if p := errObj.Get("param"); p.Exists() {
			s := p.String()
			if s != "" {
				param = &s
			}
		}
	}

	// Generic codes like "bad_request" or "validation_error" should be upgraded
	// to a specific canonical code when the message gives a clear hint (e.g.
	// "maximum context length" should become "context_length_exceeded").
	if isOverridableCode(code) {
		code = translator.InferCodeFromMessage(message, statusCode, code)
	}
	// InferCodeFromMessage may leave a non-canonical default when the message
	// gives no clue; normalize the final code to a canonical value.
	code = canonicalizeCode(code, statusCode)
	errType = typeForCode(code, errType)

	resp := translator.OpenAIError{}
	resp.Error.Message = message
	resp.Error.Type = errType
	resp.Error.Code = code
	if param != nil {
		resp.Error.Param = param
	}
	b, _ := json.Marshal(resp)
	return b
}

func mapOpenAIErrorType(t string) string {
	switch strings.ToLower(t) {
	case "invalid_request_error":
		return "invalid_request_error"
	case "authentication_error":
		return "authentication_error"
	case "permission_error":
		return "permission_error"
	case "rate_limit_error":
		return "rate_limit_error"
	case "server_error":
		return "server_error"
	case "billing_error":
		return "billing_error"
	case "not_found_error":
		return "not_found_error"
	}
	return ""
}

func isOverridableCode(code string) bool {
	switch strings.ToLower(code) {
	case "bad_request", "invalid_request_error", "validation_error",
		"server_error", "internal_server_error":
		return true
	}
	return !canonicalOpenAICodes[strings.ToLower(code)]
}

func canonicalizeCode(code string, statusCode int) string {
	lower := strings.ToLower(code)
	if canonicalOpenAICodes[lower] {
		return lower
	}
	tmpType, tmpCode := translator.MapHTTPStatus(statusCode)
	_ = tmpType
	return tmpCode
}

func typeForCode(code, fallback string) string {
	switch strings.ToLower(code) {
	case "context_length_exceeded", "bad_request", "model_not_found", "model_not_supported":
		return "invalid_request_error"
	case "rate_limit_exceeded", "insufficient_quota":
		return "rate_limit_error"
	case "invalid_api_key":
		return "authentication_error"
	case "content_filter":
		return "invalid_request_error"
	case "payment_required":
		return "billing_error"
	case "permission_error":
		return "permission_error"
	case "internal_server_error", "bad_gateway", "service_unavailable", "gateway_timeout":
		return "server_error"
	case "not_found_error":
		return "invalid_request_error"
	}
	if fallback != "" {
		return fallback
	}
	if code == "" {
		typ, _ := translator.MapHTTPStatus(http.StatusInternalServerError)
		return typ
	}
	return "invalid_request_error"
}
