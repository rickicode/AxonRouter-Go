package executor

import (
	"context"
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

func openAIEndpoint(baseURL, endpoint string) string {
	if baseURL == "" {
		return "https://api.openai.com/v1/" + endpoint
	}
	url := strings.TrimRight(baseURL, "/")
	for _, suffix := range []string{"/chat/completions", "/responses", "/embeddings", "/models"} {
		if strings.HasSuffix(url, suffix) {
			return url
		}
	}
	return url + "/" + endpoint
}

// Execute performs a non-streaming chat completion.
func (e *OpenAIExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := openAIEndpoint(req.BaseURL, "chat/completions")

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
		return nil, fmt.Errorf("openai error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return resp, nil
}

// ExecuteStream performs a streaming chat completion.
func (e *OpenAIExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	url := openAIEndpoint(req.BaseURL, "chat/completions")

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

	return e.DoStreamRequest(ctx, "POST", url, headers, body)
}

// Embeddings performs an embedding request.
func (e *OpenAIExecutor) Embeddings(ctx context.Context, req *Request) (*Response, error) {
	url := openAIEndpoint(req.BaseURL, "embeddings")

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
	url := openAIEndpoint(req.BaseURL, "models")

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
	url := openAIEndpoint(req.BaseURL, "responses")

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
	url := openAIEndpoint(req.BaseURL, "responses")

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
