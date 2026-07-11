package executor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

// UpstreamError carries a translated upstream error response so the handler can
// return it to the client with the original HTTP status and provider body.
type UpstreamError struct {
	StatusCode int
	Body       []byte // provider/translated error body (usually JSON)
	RawBody    []byte // original raw body before translation (for logging)
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream error %d: %s", e.StatusCode, string(e.Body))
}

// IsClientError reports whether the upstream error is a 4xx client error.
func (e *UpstreamError) IsClientError() bool {
	return e.StatusCode >= 400 && e.StatusCode < 500
}

// upstreamErrorRE matches the error strings produced by BaseExecutor:
//   "openai error 400: {...}" or "stream error 400: {...}"
var upstreamErrorRE = regexp.MustCompile(`(?is)^(openai|stream) error (\d+):\s*(.*)$`)

// toCloudflareUpstreamError attempts to turn a raw executor error into an
// OpenAI-compatible UpstreamError. It only returns non-nil when the body matches
// the Cloudflare Workers AI error envelope and can be translated.
func toCloudflareUpstreamError(err error) *UpstreamError {
	if err == nil {
		return nil
	}
	m := upstreamErrorRE.FindStringSubmatch(err.Error())
	if len(m) < 4 {
		return nil
	}
	status, _ := strconv.Atoi(m[2])
	if status == 0 {
		return nil
	}
	raw := []byte(m[3])
	translated := translateCloudflareError(status, raw)
	if translated == nil {
		return nil
	}
	return &UpstreamError{
		StatusCode: status,
		Body:       translated,
		RawBody:    raw,
	}
}

// translateCloudflareError converts a Cloudflare Workers AI error body like:
//
//	{"errors":[{"message":"AiError: AiError: {\"object\":\"error\",\"message\":\"...\",\"type\":\"BadRequestError\",\"param\":null,\"code\":400} (uuid)","code":8007}],"success":false,"result":{},"messages":[]}
//
// into an OpenAI-compatible error JSON:
//
//	{"error":{"message":"...","type":"invalid_request_error","param":null,"code":"context_length_exceeded"}}
func translateCloudflareError(statusCode int, raw []byte) []byte {
	if !gjson.ValidBytes(raw) {
		return nil
	}
	msg := gjson.GetBytes(raw, "errors.0.message").String()
	if msg == "" {
		return nil
	}
	start := strings.Index(msg, "{")
	end := strings.LastIndex(msg, "}")
	if start == -1 || end <= start {
		return nil
	}
	nested := []byte(msg[start : end+1])

	var cf struct {
		Message string  `json:"message"`
		Type    string  `json:"type"`
		Param   *string `json:"param"`
		Code    int     `json:"code"`
	}
	if err := json.Unmarshal(nested, &cf); err != nil {
		return nil
	}

	// Fallback heuristic when the nested object doesn't set a type.
	typ := cf.Type
	if typ == "" && statusCode >= 400 {
		typ = "BadRequestError"
	}

	oai := map[string]any{
		"message": cf.Message,
	}

	switch typ {
	case "BadRequestError":
		oai["type"] = "invalid_request_error"
	case "UnauthorizedError":
		oai["type"] = "authentication_error"
	case "ForbiddenError":
		oai["type"] = "permission_error"
	case "NotFoundError":
		oai["type"] = "not_found_error"
	case "RateLimitError":
		oai["type"] = "rate_limit_exceeded"
	default:
		if statusCode >= 500 {
			oai["type"] = "server_error"
		} else {
			oai["type"] = "invalid_request_error"
		}
	}

	if cf.Param != nil {
		oai["param"] = *cf.Param
	}

	oai["code"] = inferOpenAICode(statusCode, cf.Message, oai["type"].(string))

	out := map[string]any{"error": oai}
	b, _ := json.Marshal(out)
	return b
}

func inferOpenAICode(statusCode int, message, oaiType string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "maximum context length") ||
		strings.Contains(lower, "context length") ||
		strings.Contains(lower, "exceeds the model's maximum"):
		return "context_length_exceeded"
	case strings.Contains(lower, "model not found") ||
		strings.Contains(lower, "no such model") ||
		strings.Contains(lower, "does not exist"):
		return "model_not_found"
	case strings.Contains(lower, "invalid api key") ||
		strings.Contains(lower, "incorrect api key"):
		return "invalid_api_key"
	case strings.Contains(lower, "insufficient_quota") ||
		strings.Contains(lower, "quota"):
		return "insufficient_quota"
	case strings.Contains(lower, "content filter") ||
		strings.Contains(lower, "safety"):
		return "content_filter"
	}

	if statusCode == http.StatusTooManyRequests {
		return "rate_limit_exceeded"
	}

	switch oaiType {
	case "invalid_request_error":
		return "invalid_request_error"
	case "authentication_error":
		return "invalid_api_key"
	case "permission_error":
		return "permission_error"
	case "not_found_error":
		return "not_found_error"
	case "rate_limit_exceeded":
		return "rate_limit_exceeded"
	default:
		return "server_error"
	}
}
