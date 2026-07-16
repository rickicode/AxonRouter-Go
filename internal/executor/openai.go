package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/providercfg"
	"github.com/rickicode/AxonRouter-Go/internal/tokenizer"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// OpenAIExecutor handles OpenAI-compatible providers.
type OpenAIExecutor struct {
	*BaseExecutor
}

// ImageGenerator is implemented by executors that can generate images
// through an OpenAI-compatible /images/generations endpoint.
type ImageGenerator interface {
	Images(ctx context.Context, req *Request) (*Response, error)
}

// NewOpenAIExecutor creates a new OpenAI executor.
func NewOpenAIExecutor(base *BaseExecutor) *OpenAIExecutor {
	return &OpenAIExecutor{BaseExecutor: base}
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

// sanitizeCFRequest is retained for callers that historically embedded the
// Cloudflare-specific rules. It now delegates to the config-driven sanitizer
// using the seeded "cf" defaults so behaviour is unchanged.
func sanitizeCFRequest(body []byte) []byte {
	return sanitizeRequestWithCompatibility(body, providercfg.CompatibilityFor("cf"))
}

// sanitizeRequestWithCompatibility applies provider-specific OpenAI-compatible
// request normalisation based on a Compatibility config. It replaces the
// previously hard-coded Cloudflare and Bedrock quirks with values that can be
// overridden per provider outside the binary.
func sanitizeRequestWithCompatibility(body []byte, c providercfg.Compatibility) []byte {
	// Fast path: if no message has array content and the provider does not
	// require flattening, avoid the expensive map[string]any round-trip.
	messages := gjson.GetBytes(body, "messages")
	needFlatten := c.FlattenContentArrays && messages.Exists() && messages.IsArray() && messagesNeedSanitize(messages.Array())
	if !needFlatten {
		modelNode := gjson.GetBytes(body, "model")
		model := normalizeModelName(modelNode.String(), c)
		if model != modelNode.String() {
			body, _ = sjson.SetBytes(body, "model", model)
		}

		if reNode := gjson.GetBytes(body, "reasoning_effort"); reNode.Type == gjson.String {
			re := strings.ToLower(strings.TrimSpace(reNode.String()))
			if len(c.ReasoningLevels) > 0 {
				if c.HasReasoning(re) {
					if re != reNode.String() {
						body, _ = sjson.SetBytes(body, "reasoning_effort", re)
					}
				} else {
					body, _ = sjson.DeleteBytes(body, "reasoning_effort")
				}
			}
		}

		if c.MaxTokensCap > 0 {
			maxCap := c.MaxTokensCap
			if c.ReasoningMaxTokensCap > 0 && IsReasoningModel(model) {
				maxCap = c.ReasoningMaxTokensCap
			}
			if mt := gjson.GetBytes(body, "max_tokens"); !mt.Exists() || mt.Int() <= 0 || mt.Int() > int64(maxCap) {
				body, _ = sjson.SetBytes(body, "max_tokens", maxCap)
			}
		}
		return body
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}

	model, _ := req["model"].(string)
	model = normalizeModelName(model, c)
	if model != "" {
		req["model"] = model
	}

	if re, ok := req["reasoning_effort"].(string); ok {
		re = strings.ToLower(strings.TrimSpace(re))
		if len(c.ReasoningLevels) > 0 {
			if c.HasReasoning(re) {
				req["reasoning_effort"] = re
			} else {
				delete(req, "reasoning_effort")
			}
		}
	}

	if c.MaxTokensCap > 0 {
		maxCap := c.MaxTokensCap
		if c.ReasoningMaxTokensCap > 0 && IsReasoningModel(model) {
			maxCap = c.ReasoningMaxTokensCap
		}
		if current, ok := req["max_tokens"].(float64); !ok || current <= 0 || current > float64(maxCap) {
			req["max_tokens"] = maxCap
		}
	}

	if messages, ok := req["messages"].([]any); ok && c.FlattenContentArrays {
		req["messages"] = sanitizeMessages(messages)
	}

	out, err := json.Marshal(req)
	if err != nil {
		return body
	}
	return out
}

// normalizeModelName applies the provider-specific model prefix and strips any
// configured provider prefix from the model ID.
func normalizeModelName(model string, c providercfg.Compatibility) string {
	if model == "" {
		return ""
	}
	if c.StripProviderPrefix != "" {
		model = strings.TrimPrefix(model, c.StripProviderPrefix)
	}
	if c.ModelPrefix == "" {
		return model
	}
	if strings.HasPrefix(model, c.ModelPrefix) {
		return model
	}
	// Support gateway IDs like "cf/author/model" for a "@cf/" prefix.
	if len(c.ModelPrefix) > 1 && strings.HasPrefix(model, c.ModelPrefix[1:]) {
		return "@" + model
	}
	return c.ModelPrefix + model
}

// messagesNeedSanitize reports whether any message contains an array content
// that may need flattening or tool_result conversion.
func messagesNeedSanitize(messages []gjson.Result) bool {
	for _, msg := range messages {
		if msg.Get("content").IsArray() {
			return true
		}
	}
	return false
}

func sanitizeMessages(messages []any) []any {
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

		// Flatten content to a plain string for providers that reject arrays.
		var textParts []string
		for _, b := range otherBlocks {
			typ, _ := b["type"].(string)
			if typ == "text" {
				if s, ok := b["text"].(string); ok {
					textParts = append(textParts, s)
				}
			}
			// image_url and other non-text blocks are dropped.
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
	openRouterHeaders(headers, req.Provider, req.ProviderSpecificData)

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

// CountTokens counts input tokens for an OpenAI-compatible provider after
// translating an Anthropic /v1/messages/count_tokens request to OpenAI Chat
// Completions format. It returns an Anthropic-style {"input_tokens": N} body.
func (e *OpenAIExecutor) CountTokens(ctx context.Context, req *Request) (*Response, error) {
	modelName := req.Model
	if modelName == "" {
		modelName = JSONGet(req.Body, "model")
	}

	translated := registry.Request(string(FormatClaude), string(FormatOpenAI), modelName, req.Body, false)

	enc, err := tokenizer.CodecForModel(modelName)
	if err != nil {
		return nil, fmt.Errorf("tokenizer init failed: %w", err)
	}

	count, err := tokenizer.CountOpenAIChatTokens(enc, translated)
	if err != nil {
		return nil, fmt.Errorf("token counting failed: %w", err)
	}

	body := fmt.Sprintf(`{"input_tokens":%d}`, count)
	return &Response{
		StatusCode: http.StatusOK,
		Body:       []byte(body),
	}, nil
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
	openRouterHeaders(headers, req.Provider, req.ProviderSpecificData)

	return e.DoStreamRequestWithConfig(ContextWithProvider(ctx, req.Provider), "POST", url, headers, body, req.StreamConfig)
}

// Embeddings performs an embedding request.
func (e *OpenAIExecutor) Embeddings(ctx context.Context, req *Request) (*Response, error) {
	url, err := openAIEndpoint(req.BaseURL, "embeddings", req.ProviderSpecificData)
	if err != nil {
		return nil, err
	}

	body := req.Body
	// Providers with a model prefix (e.g. Cloudflare Workers AI) need the full
	// upstream model path for /v1/embeddings.
	if req.Provider != "" {
		c := providercfg.CompatibilityFor(req.Provider)
		model := normalizeModelName(gjson.GetBytes(body, "model").String(), c)
		body = JSONSet(body, "model", model)
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)
	openRouterHeaders(headers, req.Provider, req.ProviderSpecificData)

	resp, err := e.DoRequest(ctx, "POST", url, headers, body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, &UpstreamError{StatusCode: resp.StatusCode, Body: resp.Body, RawBody: resp.Body, Headers: resp.Headers}
	}

	return resp, nil
}

// Images performs an image generation request.
func (e *OpenAIExecutor) Images(ctx context.Context, req *Request) (*Response, error) {
	url, err := openAIEndpoint(req.BaseURL, "images/generations", req.ProviderSpecificData)
	if err != nil {
		return nil, err
	}

	body := req.Body
	body = JSONSet(body, "stream", false)

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)
	openRouterHeaders(headers, req.Provider, req.ProviderSpecificData)

	resp, err := e.DoRequest(ctx, "POST", url, headers, body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		upErr := &UpstreamError{StatusCode: resp.StatusCode, Body: resp.Body, RawBody: resp.Body, Headers: resp.Headers}
		upErr.TranslateErrorBody(req.Provider)
		return nil, upErr
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
	openRouterHeaders(headers, req.Provider, req.ProviderSpecificData)

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
	openRouterHeaders(headers, req.Provider, req.ProviderSpecificData)

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
	openRouterHeaders(headers, req.Provider, req.ProviderSpecificData)

	return e.DoStreamRequestWithConfig(ContextWithProvider(ctx, req.Provider), "POST", url, headers, body, req.StreamConfig)
}
