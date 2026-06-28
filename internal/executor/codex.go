package executor

import (
	"context"
	"fmt"
)

// CodexExecutor handles OpenAI Codex (Responses API) with OAuth tokens.
type CodexExecutor struct {
	*BaseExecutor
}

// NewCodexExecutor creates a new Codex executor.
func NewCodexExecutor(base *BaseExecutor) *CodexExecutor {
	return &CodexExecutor{BaseExecutor: base}
}

// Execute performs a non-streaming Codex Responses API call.
func (e *CodexExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://chatgpt.com/backend-api/codex/responses"
	}

	body := req.Body
	body = JSONSet(body, "stream", false)

	headers := map[string]string{
		"Content-Type":    "application/json",
		"Accept":          "application/json",
		"Authorization":   "Bearer " + req.AccessToken,
		"User-Agent":      "codex_cli_rs/0.42.0 (Debian 12.9; x86_64)",
		"Openai-Beta":     "responses=experimental",
	}
	if req.APIKey != "" && req.AccessToken == "" {
		headers["Authorization"] = "Bearer " + req.APIKey
	}

	resp, err := e.DoRequest(ctx, "POST", url, headers, body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("codex error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return resp, nil
}

// ExecuteStream performs a streaming Codex Responses API call.
func (e *CodexExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://chatgpt.com/backend-api/codex/responses"
	}

	body := req.Body
	body = JSONSet(body, "stream", true)

	headers := map[string]string{
		"Content-Type":    "application/json",
		"Accept":          "text/event-stream",
		"Cache-Control":   "no-cache",
		"Authorization":   "Bearer " + req.AccessToken,
		"User-Agent":      "codex_cli_rs/0.42.0 (Debian 12.9; x86_64)",
		"Openai-Beta":     "responses=experimental",
	}
	if req.APIKey != "" && req.AccessToken == "" {
		headers["Authorization"] = "Bearer " + req.APIKey
	}

	return e.DoStreamRequest(ctx, "POST", url, headers, body)
}
