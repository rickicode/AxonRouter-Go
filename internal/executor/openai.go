package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// OpenAIExecutor handles OpenAI-compatible providers.
type OpenAIExecutor struct {
	*BaseExecutor
}

// NewOpenAIExecutor creates a new OpenAI executor.
func NewOpenAIExecutor(base *BaseExecutor) *OpenAIExecutor {
	return &OpenAIExecutor{BaseExecutor: base}
}

// openRouterHeaders adds OpenRouter attribution headers to the headers map
// if the provider is "openrouter". OpenRouter uses HTTP-Referer and X-Title
// for app attribution and rate-limit tracking.
func openRouterHeaders(headers map[string]string, provider string) {
	if provider == "openrouter" {
		headers["HTTP-Referer"] = "https://endpoint-proxy.local"
		headers["X-Title"] = "Endpoint Proxy"
	}
}

// IsReasoningModel detects reasoning models generically (not CF-specific).
// It checks catalog strip flags when available, otherwise falls back to
// naming heuristics matching OmniRoute stripList behaviour.
func IsReasoningModel(model string) bool {
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, "r1"):
		return true
	case strings.Contains(lower, "qwq"):
		return true
	case strings.Contains(lower, "kimi-k2.5"), strings.Contains(lower, "kimi-k2.6"), strings.Contains(lower, "kimi-k2.7"):
		return true
	case strings.Contains(lower, "glm-5."):
		return true
	case strings.Contains(lower, "glm-4.7"):
		return true
	}
	return false
}

// sanitizeCFRequest enforces Cloudflare Workers AI constraints:
// - max_tokens cap: 4096 for reasoning models, 8192 otherwise.
// - message content arrays are flattened to a plain string (CF rejects part arrays).
// - tool_result blocks are converted into role:tool messages.
//
// This matches AMRouter chatCore.js san behaviour for the cloudflare-ai provider.
func sanitizeCFRequest(body []byte) []byte {
	// Fast path: if no message has an array content, we only need to adjust
	// top-level fields (model prefix, reasoning_effort, max_tokens). That lets
	// us skip the expensive full map[string]any round-trip and the message
	// allocation churn for the common string-content case.
	messages := gjson.GetBytes(body, "messages")
	if messages.Exists() && messages.IsArray() && !cfMessagesNeedSanitize(messages.Array()) {
		modelNode := gjson.GetBytes(body, "model")
		model := normalizeCFModelName(modelNode.String())
		if model != modelNode.String() {
			body, _ = sjson.SetBytes(body, "model", model)
		}

		if reNode := gjson.GetBytes(body, "reasoning_effort"); reNode.Type == gjson.String {
			re := strings.ToLower(strings.TrimSpace(reNode.String()))
			switch re {
			case "none", "low", "medium", "high", "max":
				if re != reNode.String() {
					body, _ = sjson.SetBytes(body, "reasoning_effort", re)
				}
			default:
				body, _ = sjson.DeleteBytes(body, "reasoning_effort")
			}
		}

		maxCap := 8192
		if IsReasoningModel(model) {
			maxCap = 4096
		}
		if mt := gjson.GetBytes(body, "max_tokens"); !mt.Exists() || mt.Int() <= 0 || mt.Int() > int64(maxCap) {
			body, _ = sjson.SetBytes(body, "max_tokens", maxCap)
		}
		return body
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}

	model, _ := req["model"].(string)
	// CF Workers AI requires @cf/ prefix on model names. The caller may pass
	// the gateway model ID (cf/author/model) or just the upstream name
	// (author/model). Normalize so the upstream always sees exactly
	// @cf/author/model — never @cf/cf/... or double prefixes.
	if model != "" {
		switch {
		case strings.HasPrefix(model, "@cf/"):
			// already normalized
		case strings.HasPrefix(model, "cf/"):
			// gateway full ID like cf/author/model
			model = "@" + model
		default:
			// model name only like author/model
			model = "@cf/" + model
		}
		req["model"] = model
	}

	// CF Workers AI may route to backends (e.g. SGLang) that only accept
	// reasoning_effort values: none, low, medium, high, max. Reject unknown
	// values by dropping the field instead of letting upstream return 400.
	if re, ok := req["reasoning_effort"].(string); ok {
		re = strings.ToLower(strings.TrimSpace(re))
		switch re {
		case "none", "low", "medium", "high", "max":
			req["reasoning_effort"] = re
		default:
			delete(req, "reasoning_effort")
		}
	}

	maxCap := 8192
	if IsReasoningModel(model) {
		maxCap = 4096
	}
	if current, ok := req["max_tokens"].(float64); !ok || current <= 0 || current > float64(maxCap) {
		req["max_tokens"] = maxCap
	}

	if messages, ok := req["messages"].([]any); ok {
		req["messages"] = sanitizeCFMessages(messages)
	}

	out, err := json.Marshal(req)
	if err != nil {
		return body
	}
	return out
}

func normalizeCFModelName(model string) string {
	if model == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(model, "@cf/"):
		return model
	case strings.HasPrefix(model, "cf/"):
		return "@" + model
	default:
		return "@cf/" + model
	}
}

// cfMessagesNeedSanitize reports whether any message contains an array
// content that may need flattening or tool_result conversion.
func cfMessagesNeedSanitize(messages []gjson.Result) bool {
	for _, msg := range messages {
		if msg.Get("content").IsArray() {
			return true
		}
	}
	return false
}

func sanitizeCFMessages(messages []any) []any {
	var sanitized []any
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			sanitized = append(sanitized, raw)
			continue
		}
		content, ok := msg["content"].([]any)
		if !ok {
			sanitized = append(sanitized, msg)
			continue
		}

		var toolResults []map[string]any
		var otherBlocks []map[string]any
		for _, b := range content {
			block, ok := b.(map[string]any)
			if !ok {
				continue
			}
			typ, _ := block["type"].(string)
			if typ == "tool_result" {
				toolResults = append(toolResults, block)
			} else {
				otherBlocks = append(otherBlocks, block)
			}
		}

		for _, tr := range toolResults {
			var text string
			switch v := tr["content"].(type) {
			case string:
				text = v
			case []any:
				parts := make([]string, 0, len(v))
				for _, c := range v {
					if cm, ok := c.(map[string]any); ok {
						if t, ok := cm["type"].(string); ok && t == "text" {
							if s, ok := cm["text"].(string); ok {
								parts = append(parts, s)
							}
						}
					}
				}
				text = strings.Join(parts, "\n")
			default:
				if v != nil {
					b, _ := json.Marshal(v)
					text = string(b)
				}
			}
			toolCallID, _ := tr["tool_use_id"].(string)
			if toolCallID == "" {
				toolCallID, _ = tr["tool_call_id"].(string)
			}
			sanitized = append(sanitized, map[string]any{
				"role":         "tool",
				"tool_call_id": toolCallID,
				"content":      text,
			})
		}

		// If this message only contained tool_result blocks, drop the original
		// message and keep only the converted role:tool messages.
		if len(otherBlocks) == 0 {
			continue
		}

		// Flatten content to a plain string for CF Workers AI.
		// OmniRoute #2539: Workers AI /ai/v1/chat/completions rejects
		// content-part arrays like [{type:"text",text}] with HTTP 400.
		var textParts []string
		for _, b := range otherBlocks {
			typ, _ := b["type"].(string)
			if typ == "text" {
				if s, ok := b["text"].(string); ok {
					textParts = append(textParts, s)
				}
			}
			// image_url and other non-text blocks are dropped: CF chat
			// completions endpoint does not accept array content.
		}

		newMsg := make(map[string]any, len(msg))
		for k, v := range msg {
			newMsg[k] = v
		}
		newMsg["content"] = strings.Join(textParts, "")
		sanitized = append(sanitized, newMsg)
	}
	return sanitized
}

// openAIEndpoint resolves the full upstream URL for an OpenAI-compatible provider.
// When the base URL contains {accountId}, the placeholder is resolved from psd
// (provider_specific_data), then from the CLOUDFLARE_ACCOUNT_ID env var.
// Returns an error if the placeholder cannot be resolved — matching OmniRoute's
// CloudflareAIExecutor.buildUrl() which throws on missing accountId.
func openAIEndpoint(baseURL, endpoint string, psd map[string]string) (string, error) {
	if baseURL == "" {
		return "https://api.openai.com/v1/" + endpoint, nil
	}
	url := strings.TrimRight(baseURL, "/")
	// Resolve {accountId} template — Cloudflare Workers AI pattern.
	// Match OmniRoute: PSD → top-level creds → env var → error.
	if strings.Contains(url, "{accountId}") {
		accountID := psd["accountId"]
		if accountID == "" {
			accountID = os.Getenv("CLOUDFLARE_ACCOUNT_ID")
		}
		if accountID == "" {
			return "", fmt.Errorf(
				"cloudflare Workers AI requires an Account ID. " +
					"Add it in provider settings or set CLOUDFLARE_ACCOUNT_ID env var. " +
					"Find it at: https://dash.cloudflare.com (right sidebar)")
		}
		url = strings.ReplaceAll(url, "{accountId}", accountID)
	}
	// If the base_url already ends with the requested endpoint, return as-is.
	if strings.HasSuffix(url, "/"+endpoint) {
		return url, nil
	}
	// If the base_url ends with a DIFFERENT known endpoint, strip it first.
	for _, suffix := range []string{"/chat/completions", "/responses", "/embeddings", "/models"} {
		if strings.HasSuffix(url, suffix) {
			url = strings.TrimSuffix(url, suffix)
			break
		}
	}
	return url + "/" + endpoint, nil
}

// Execute performs a non-streaming chat completion.
func (e *OpenAIExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url, err := openAIEndpoint(req.BaseURL, "chat/completions", req.ProviderSpecificData)
	if err != nil {
		return nil, err
	}

	body := req.Body
	// Ensure stream is false
	body = JSONSet(body, "stream", false)

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)
	openRouterHeaders(headers, req.Provider)

	resp, err := e.DoRequest(ctx, "POST", url, headers, body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		upErr := &UpstreamError{
			StatusCode: resp.StatusCode,
			Body:       resp.Body,
			RawBody:    resp.Body,
			Headers:    resp.Headers,
		}
		upErr.TranslateErrorBody(req.Provider)
		return nil, upErr
	}

	return resp, nil
}

// ExecuteStream performs a streaming chat completion.
func (e *OpenAIExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	url, err := openAIEndpoint(req.BaseURL, "chat/completions", req.ProviderSpecificData)
	if err != nil {
		return nil, err
	}

	body := req.Body
	// Ensure stream is true
	body = JSONSet(body, "stream", true)

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Accept":        "text/event-stream",
		"Cache-Control": "no-cache",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)
	openRouterHeaders(headers, req.Provider)

	return e.DoStreamRequestWithConfig(ctx, "POST", url, headers, body, req.StreamConfig)
}

// Embeddings performs an embedding request.
func (e *OpenAIExecutor) Embeddings(ctx context.Context, req *Request) (*Response, error) {
	url, err := openAIEndpoint(req.BaseURL, "embeddings", req.ProviderSpecificData)
	if err != nil {
		return nil, err
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)
	openRouterHeaders(headers, req.Provider)

	resp, err := e.DoRequest(ctx, "POST", url, headers, req.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, &UpstreamError{StatusCode: resp.StatusCode, Body: resp.Body, RawBody: resp.Body, Headers: resp.Headers}
	}

	return resp, nil
}

// Models returns available models for OpenAI.
func (e *OpenAIExecutor) Models(ctx context.Context, req *Request) (*Response, error) {
	url, err := openAIEndpoint(req.BaseURL, "models", req.ProviderSpecificData)
	if err != nil {
		return nil, err
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)
	openRouterHeaders(headers, req.Provider)

	resp, err := e.DoRequest(ctx, "GET", url, headers, nil)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai models error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return resp, nil
}

// Responses performs an OpenAI Responses API call (for Codex-style).
func (e *OpenAIExecutor) Responses(ctx context.Context, req *Request) (*Response, error) {
	url, err := openAIEndpoint(req.BaseURL, "responses", req.ProviderSpecificData)
	if err != nil {
		return nil, err
	}

	body := req.Body
	body = JSONSet(body, "stream", false)

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)
	openRouterHeaders(headers, req.Provider)

	resp, err := e.DoRequest(ctx, "POST", url, headers, body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("responses error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return resp, nil
}

// ResponsesStream performs a streaming Responses API call.
func (e *OpenAIExecutor) ResponsesStream(ctx context.Context, req *Request) (*StreamResult, error) {
	url, err := openAIEndpoint(req.BaseURL, "responses", req.ProviderSpecificData)
	if err != nil {
		return nil, err
	}

	body := req.Body
	body = JSONSet(body, "stream", true)

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Accept":        "text/event-stream",
		"Cache-Control": "no-cache",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)
	openRouterHeaders(headers, req.Provider)

	return e.DoStreamRequestWithConfig(ctx, "POST", url, headers, body, req.StreamConfig)
}
