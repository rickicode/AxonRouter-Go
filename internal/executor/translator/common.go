package translator

import (
	"encoding/json"
	"net/http"
	"strings"
)

// OpenAIError represents the standard OpenAI-compatible error envelope.
type OpenAIError struct {
	Error struct {
		Message string  `json:"message"`
		Type    string  `json:"type"`
		Param   *string `json:"param,omitempty"`
		Code    string  `json:"code"`
	} `json:"error"`
}

// BuildError builds a minimal OpenAI-compatible error body.
func BuildError(message, errType, code string) []byte {
	body := OpenAIError{}
	body.Error.Message = message
	body.Error.Type = errType
	body.Error.Code = code
	b, _ := json.Marshal(body)
	return b
}

// MapHTTPStatus maps an HTTP status code to OpenAI-compatible type/code defaults.
func MapHTTPStatus(statusCode int) (errType, code string) {
	switch statusCode {
	case http.StatusBadRequest:
		return "invalid_request_error", "bad_request"
	case http.StatusUnauthorized:
		return "authentication_error", "invalid_api_key"
	case http.StatusPaymentRequired:
		return "billing_error", "payment_required"
	case http.StatusForbidden:
		return "permission_error", "insufficient_quota"
	case http.StatusNotFound:
		return "invalid_request_error", "model_not_found"
	case http.StatusNotAcceptable:
		return "invalid_request_error", "model_not_supported"
	case http.StatusTooManyRequests:
		return "rate_limit_error", "rate_limit_exceeded"
	case http.StatusInternalServerError:
		return "server_error", "internal_server_error"
	case http.StatusBadGateway:
		return "server_error", "bad_gateway"
	case http.StatusServiceUnavailable:
		return "server_error", "service_unavailable"
	case http.StatusGatewayTimeout:
		return "server_error", "gateway_timeout"
	default:
		if statusCode >= 500 {
			return "server_error", "internal_server_error"
		}
		if statusCode >= 400 {
			return "invalid_request_error", "bad_request"
		}
		return "server_error", "internal_server_error"
	}
}

// InferCodeFromMessage inspects the message text for known provider phrases
// and returns a provider-specific OpenAI code when possible.
func InferCodeFromMessage(message string, statusCode int, defaultCode string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "maximum context length") ||
		strings.Contains(lower, "context length") ||
		strings.Contains(lower, "exceeds the model's maximum") ||
		strings.Contains(lower, "prompt is too long") ||
		strings.Contains(lower, "too long"):
		return "context_length_exceeded"
	case strings.Contains(lower, "model not found"),
		strings.Contains(lower, "no such model"),
		strings.Contains(lower, "does not exist"):
		return "model_not_found"
	case strings.Contains(lower, "invalid api key"),
		strings.Contains(lower, "incorrect api key"):
		return "invalid_api_key"
	case strings.Contains(lower, "insufficient_quota"),
		strings.Contains(lower, "insufficient quota"):
		return "insufficient_quota"
	case strings.Contains(lower, "quota"):
		return "insufficient_quota"
	case strings.Contains(lower, "content filter"),
		strings.Contains(lower, "safety"):
		return "content_filter"
	}
	if statusCode == http.StatusTooManyRequests {
		return "rate_limit_exceeded"
	}
	return defaultCode
}
