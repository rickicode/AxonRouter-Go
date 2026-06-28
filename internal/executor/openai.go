package executor

import (
	"context"
	"encoding/json"
	"fmt"
)

// OpenAIExecutor handles OpenAI-compatible providers.
type OpenAIExecutor struct {
	*BaseExecutor
}

// NewOpenAIExecutor creates a new OpenAI executor.
func NewOpenAIExecutor(base *BaseExecutor) *OpenAIExecutor {
	return &OpenAIExecutor{BaseExecutor: base}
}

// Execute performs a non-streaming chat completion.
func (e *OpenAIExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://api.openai.com/v1/chat/completions"
	}

	body := req.Body
	// Ensure stream is false
	body = JSONSet(body, "stream", false)

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)

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
	url := req.BaseURL
	if url == "" {
		url = "https://api.openai.com/v1/chat/completions"
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

	return e.DoStreamRequest(ctx, "POST", url, headers, body)
}

// Embeddings performs an embedding request.
func (e *OpenAIExecutor) Embeddings(ctx context.Context, req *Request) (*Response, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://api.openai.com/v1/embeddings"
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)

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
	url := req.BaseURL
	if url == "" {
		url = "https://api.openai.com/v1/models"
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)

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
	url := req.BaseURL
	if url == "" {
		url = "https://api.openai.com/v1/responses"
	}

	body := req.Body
	body = JSONSet(body, "stream", false)

	headers := map[string]string{
		"Content-Type": "application/json",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)

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
	url := req.BaseURL
	if url == "" {
		url = "https://api.openai.com/v1/responses"
	}

	body := req.Body
	body = JSONSet(body, "stream", true)

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Accept":        "text/event-stream",
		"Cache-Control": "no-cache",
	}
	SetAuthHeader(headers, req.APIKey, req.AccessToken)

	return e.DoStreamRequest(ctx, "POST", url, headers, body)
}

// parseOpenAIUsage extracts token usage from an OpenAI response.
func parseOpenAIUsage(body []byte) (inputTokens, outputTokens, reasoningTokens int64) {
	var resp struct {
		Usage *struct {
			PromptTokens            int64 `json:"prompt_tokens"`
			CompletionTokens        int64 `json:"completion_tokens"`
			TotalTokens             int64 `json:"total_tokens"`
			PromptTokensDetails     *struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			CompletionTokensDetails *struct {
				ReasoningTokens int64 `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.Usage == nil {
		return 0, 0, 0
	}
	inputTokens = resp.Usage.PromptTokens
	outputTokens = resp.Usage.CompletionTokens
	if resp.Usage.CompletionTokensDetails != nil {
		reasoningTokens = resp.Usage.CompletionTokensDetails.ReasoningTokens
	}
	return
}
