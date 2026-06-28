package executor

import (
	"context"
	"fmt"
)

// KiroExecutor handles AWS Kiro API.
type KiroExecutor struct {
	*BaseExecutor
}

// NewKiroExecutor creates a new Kiro executor.
func NewKiroExecutor(base *BaseExecutor) *KiroExecutor {
	return &KiroExecutor{BaseExecutor: base}
}

// Execute performs a non-streaming Kiro request.
func (e *KiroExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://kiro-api.aws.amazon.com/v1/chat/completions"
	}

	body := req.Body
	body = JSONSet(body, "stream", false)

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + req.AccessToken,
	}
	if req.APIKey != "" && req.AccessToken == "" {
		headers["Authorization"] = "Bearer " + req.APIKey
	}

	resp, err := e.DoRequest(ctx, "POST", url, headers, body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("kiro error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return resp, nil
}

// ExecuteStream performs a streaming Kiro request.
func (e *KiroExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://kiro-api.aws.amazon.com/v1/chat/completions"
	}

	body := req.Body
	body = JSONSet(body, "stream", true)

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Accept":        "text/event-stream",
		"Cache-Control": "no-cache",
		"Authorization": "Bearer " + req.AccessToken,
	}
	if req.APIKey != "" && req.AccessToken == "" {
		headers["Authorization"] = "Bearer " + req.APIKey
	}

	return e.DoStreamRequest(ctx, "POST", url, headers, body)
}
