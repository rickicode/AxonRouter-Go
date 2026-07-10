package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// isCFReasoningModel mirrors AMRouter reasoning-model detection for
// Cloudflare Workers AI strip flags (thinking).
func isCFReasoningModel(model string) bool {
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, "r1"):
		return true
	case strings.Contains(lower, "qwq"):
		return true
	case strings.Contains(lower, "kimi-k2.5"), strings.Contains(lower, "kimi-k2.6"):
		return true
	case strings.Contains(lower, "glm-5."):
		return true
	case strings.Contains(lower, "glm-4.7"):
		return true
	}
	return false
}

// sanitizeCFRequest enforces Cloudflare Workers AI constraints:
//   - max_tokens cap: 4096 for reasoning models, 8192 otherwise.
//   - message content arrays may only contain {type:"text"} or {type:"image_url"}.
//   - tool_result blocks are converted into role:tool messages.
//   - A single remaining text block is collapsed to a plain string.
//
// This matches AMRouter chatCore.js san behaviour for the cloudflare-ai provider.
func sanitizeCFRequest(body []byte) []byte {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}

	model, _ := req["model"].(string)
	maxCap := 8192
	if isCFReasoningModel(model) {
		maxCap = 4096
	}
	if current, ok := req["max_tokens"].(float64); !ok || current == 0 || current > float64(maxCap) {
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
				"role":        "tool",
				"tool_call_id": toolCallID,
				"content":     text,
			})
		}

		var cfSafe []map[string]any
		for _, b := range otherBlocks {
			typ, _ := b["type"].(string)
			if typ == "text" || typ == "image_url" {
				if typ == "text" {
					text, _ := b["text"].(string)
					cfSafe = append(cfSafe, map[string]any{"type": "text", "text": text})
				} else {
					cfSafe = append(cfSafe, b)
				}
			}
		}

		// If this message only contained tool_result blocks, drop the original
		// message and keep only the converted role:tool messages.
		if len(otherBlocks) == 0 {
			continue
		}

		newMsg := make(map[string]any, len(msg))
		for k, v := range msg {
			newMsg[k] = v
		}
		switch len(cfSafe) {
		case 0:
			newMsg["content"] = ""
		case 1:
			if cfSafe[0]["type"] == "text" {
				newMsg["content"] = cfSafe[0]["text"]
			} else {
				newMsg["content"] = cfSafe
			}
		default:
			newMsg["content"] = cfSafe
		}
		sanitized = append(sanitized, newMsg)
	}
	return sanitized
}


func openAIEndpoint(baseURL, endpoint string, psd map[string]string) string {
	if baseURL == "" {
		return "https://api.openai.com/v1/" + endpoint
	}
	url := strings.TrimRight(baseURL, "/")
	// Resolve {accountId} template from provider_specific_data
	if strings.Contains(url, "{accountId}") {
		if id, ok := psd["accountId"]; ok && id != "" {
			url = strings.ReplaceAll(url, "{accountId}", id)
		}
	}
	for _, suffix := range []string{"/chat/completions", "/responses", "/embeddings", "/models"} {
		if strings.HasSuffix(url, suffix) {
			return url
		}
	}
	return url + "/" + endpoint
}


// Execute performs a non-streaming chat completion.
func (e *OpenAIExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := openAIEndpoint(req.BaseURL, "chat/completions", req.ProviderSpecificData)

	body := req.Body
	if req.Provider == "cf" {
		body = sanitizeCFRequest(body)
	}
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
		return nil, fmt.Errorf("openai error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return resp, nil
}

// ExecuteStream performs a streaming chat completion.
func (e *OpenAIExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	url := openAIEndpoint(req.BaseURL, "chat/completions", req.ProviderSpecificData)

	body := req.Body
	if req.Provider == "cf" {
		body = sanitizeCFRequest(body)
	}
	// Ensure stream is true
	body = JSONSet(body, "stream", true)

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Accept":        "text/event-stream",
		"Cache-Control": "no-cache",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)
	openRouterHeaders(headers, req.Provider)

	return e.DoStreamRequest(ctx, "POST", url, headers, body)
}

// Embeddings performs an embedding request.
func (e *OpenAIExecutor) Embeddings(ctx context.Context, req *Request) (*Response, error) {
	url := openAIEndpoint(req.BaseURL, "embeddings", req.ProviderSpecificData)


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
		return nil, fmt.Errorf("openai embeddings error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return resp, nil
}

// Models returns available models for OpenAI.
func (e *OpenAIExecutor) Models(ctx context.Context, req *Request) (*Response, error) {
	url := openAIEndpoint(req.BaseURL, "models", req.ProviderSpecificData)


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
	url := openAIEndpoint(req.BaseURL, "responses", req.ProviderSpecificData)


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
	url := openAIEndpoint(req.BaseURL, "responses", req.ProviderSpecificData)

	body := req.Body
	body = JSONSet(body, "stream", true)

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Accept":        "text/event-stream",
		"Cache-Control": "no-cache",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)
	openRouterHeaders(headers, req.Provider)

	return e.DoStreamRequest(ctx, "POST", url, headers, body)
}
