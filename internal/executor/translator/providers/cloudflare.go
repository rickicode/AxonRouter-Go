package providers

import (
	"encoding/json"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/executor/translator"
	"github.com/tidwall/gjson"
)

// TranslateCloudflare converts a Cloudflare Workers AI error body like:
//
//	{"errors":[{"message":"AiError: AiError: {\"object\":\"error\",\"message\":\"...\",\"type\":\"BadRequestError\",\"param\":null,\"code\":400} (uuid)","code":8007}],"success":false,"result":{},"messages":[]}
//
// into an OpenAI-compatible error JSON:
//
//	{"error":{"message":"...","type":"invalid_request_error","param":null,"code":"context_length_exceeded"}}
func TranslateCloudflare(statusCode int, raw []byte) []byte {
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
		oai["type"] = "rate_limit_error"
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

	oai["code"] = inferCloudflareOpenAICode(statusCode, cf.Message, oai["type"].(string))

	out := map[string]any{"error": oai}
	b, _ := json.Marshal(out)
	return b
}

func inferCloudflareOpenAICode(statusCode int, message, oaiType string) string {
	if code := translator.InferCodeFromMessage(message, statusCode, ""); code != "" {
		return code
	}

	switch oaiType {
	case "invalid_request_error":
		return "invalid_request_error"
	case "authentication_error":
		return "invalid_api_key"
	case "permission_error":
		return "permission_error"
	case "not_found_error":
		return "model_not_found"
	case "rate_limit_error":
		return "rate_limit_exceeded"
	default:
		return "server_error"
	}
}
