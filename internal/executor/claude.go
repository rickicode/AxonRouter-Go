package executor

import (
	"context"
	"encoding/json"
	"fmt"
)

// ClaudeExecutor handles Anthropic Claude API.
type ClaudeExecutor struct {
	*BaseExecutor
}

// NewClaudeExecutor creates a new Claude executor.
func NewClaudeExecutor(base *BaseExecutor) *ClaudeExecutor {
	return &ClaudeExecutor{BaseExecutor: base}
}

// Execute performs a non-streaming Claude messages request.
func (e *ClaudeExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://api.anthropic.com/v1/messages"
	}

	body := req.Body
	// Ensure stream is false
	body = JSONSet(body, "stream", false)

	headers := map[string]string{
		"Content-Type":      "application/json",
		"anthropic-version": "2023-06-01",
		"x-api-key":         req.APIKey,
	}
	if req.AccessToken != "" {
		headers["Authorization"] = "Bearer " + req.AccessToken
	}

	resp, err := e.DoRequest(ctx, "POST", url, headers, body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("claude error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return resp, nil
}

// ExecuteStream performs a streaming Claude messages request.
func (e *ClaudeExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://api.anthropic.com/v1/messages"
	}

	body := req.Body
	body = JSONSet(body, "stream", true)

	headers := map[string]string{
		"Content-Type":      "application/json",
		"Accept":            "text/event-stream",
		"Cache-Control":     "no-cache",
		"anthropic-version": "2023-06-01",
		"x-api-key":         req.APIKey,
	}
	if req.AccessToken != "" {
		headers["Authorization"] = "Bearer " + req.AccessToken
	}

	return e.DoStreamRequest(ctx, "POST", url, headers, body)
}

// CountTokens performs token counting.
func (e *ClaudeExecutor) CountTokens(ctx context.Context, req *Request) (*Response, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://api.anthropic.com/v1/messages/count_tokens"
	}

	headers := map[string]string{
		"Content-Type":      "application/json",
		"anthropic-version": "2023-06-01",
		"x-api-key":         req.APIKey,
	}
	if req.AccessToken != "" {
		headers["Authorization"] = "Bearer " + req.AccessToken
	}

	resp, err := e.DoRequest(ctx, "POST", url, headers, req.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("claude count_tokens error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return resp, nil
}

// parseClaudeUsage extracts token usage from a Claude response.
func parseClaudeUsage(body []byte) (inputTokens, outputTokens int64) {
	var resp struct {
		Usage *struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.Usage == nil {
		return 0, 0
	}
	return resp.Usage.InputTokens, resp.Usage.OutputTokens
}
