package executor

import (
	"bytes"
	"context"
	"fmt"
)

// CodexExecutor handles OpenAI Codex (Responses API) with OAuth tokens.
// The Codex API is streaming-only: it rejects stream:false.
// Execute() sends stream:true to upstream and collects the SSE response internally.
type CodexExecutor struct {
	*BaseExecutor
}

// NewCodexExecutor creates a new Codex executor.
func NewCodexExecutor(base *BaseExecutor) *CodexExecutor {
	return &CodexExecutor{BaseExecutor: base}
}

// Execute performs a Codex Responses API call. The upstream always receives
// stream:true (Codex rejects non-streaming); the SSE response is collected
// and returned as a single non-streaming Response.
func (e *CodexExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://chatgpt.com/backend-api/codex/responses"
	}

	body := req.Body
	body = JSONSet(body, "stream", true)
	body = JSONSet(body, "store", false)

	headers := map[string]string{
		"Content-Type":               "application/json",
		"Accept":                     "text/event-stream",
		"Cache-Control":              "no-cache",
		"Authorization":              "Bearer " + req.AccessToken,
		"User-Agent":                 "codex_cli_rs/0.42.0 (Debian 12.9; x86_64)",
		"Openai-Beta":                "responses=experimental",
		"Originator":                 "codex_cli_rs",
		"Codex-Cli-Simplified-Flow":  "true",
	}
	if req.APIKey != "" && req.AccessToken == "" {
		headers["Authorization"] = "Bearer " + req.APIKey
	}

	streamResult, err := e.DoStreamRequest(ctx, "POST", url, headers, body)
	if err != nil {
		if upErr, ok := err.(*UpstreamError); ok {
			upErr.TranslateErrorBody(req.Provider)
		}
		return nil, err
	}

	// Collect all SSE chunks into a single response body.
	var buf bytes.Buffer
	var statusCode int
	for chunk := range streamResult.Chunks {
		if chunk.Err != nil {
			return nil, fmt.Errorf("codex stream error: %w", chunk.Err)
		}
		if chunk.Payload != nil {
			buf.Write(chunk.Payload)
		}
	}
	if streamResult.StatusCode > 0 {
		statusCode = streamResult.StatusCode
	} else {
		statusCode = 200
	}

	return &Response{
		StatusCode: statusCode,
		Body:       buf.Bytes(),
		Headers:    streamResult.Headers,
	}, nil
}

// ExecuteStream performs a streaming Codex Responses API call.
func (e *CodexExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://chatgpt.com/backend-api/codex/responses"
	}

	body := req.Body
	body = JSONSet(body, "stream", true)
	body = JSONSet(body, "store", false)

	headers := map[string]string{
		"Content-Type":               "application/json",
		"Accept":                     "text/event-stream",
		"Cache-Control":              "no-cache",
		"Authorization":              "Bearer " + req.AccessToken,
		"User-Agent":                 "codex_cli_rs/0.42.0 (Debian 12.9; x86_64)",
		"Openai-Beta":                "responses=experimental",
		"Originator":                 "codex_cli_rs",
		"Codex-Cli-Simplified-Flow":  "true",
	}
	if req.APIKey != "" && req.AccessToken == "" {
		headers["Authorization"] = "Bearer " + req.APIKey
	}

	result, err := e.DoStreamRequest(ctx, "POST", url, headers, body)
	if err != nil {
		if upErr, ok := err.(*UpstreamError); ok {
			upErr.TranslateErrorBody(req.Provider)
		}
	}
	return result, err
}
