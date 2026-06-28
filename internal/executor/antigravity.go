package executor

import (
	"context"
	"fmt"
)

// AntigravityExecutor handles Google Antigravity (Gemini Code Assist) API.
type AntigravityExecutor struct {
	*BaseExecutor
}

// NewAntigravityExecutor creates a new Antigravity executor.
func NewAntigravityExecutor(base *BaseExecutor) *AntigravityExecutor {
	return &AntigravityExecutor{BaseExecutor: base}
}

// Execute performs a non-streaming Antigravity request.
func (e *AntigravityExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://cloudcode-pa.googleapis.com/v1internal:processMessage"
	}

	body := req.Body

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + req.AccessToken,
		"User-Agent":    "google-assist-cli/1.0",
		"X-Goog-Api-Key": req.APIKey,
	}

	resp, err := e.DoRequest(ctx, "POST", url, headers, body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("antigravity error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return resp, nil
}

// ExecuteStream performs a streaming Antigravity request.
func (e *AntigravityExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://cloudcode-pa.googleapis.com/v1internal:streamProcessMessage"
	}

	body := req.Body

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Accept":        "text/event-stream",
		"Cache-Control": "no-cache",
		"Authorization": "Bearer " + req.AccessToken,
		"User-Agent":    "google-assist-cli/1.0",
		"X-Goog-Api-Key": req.APIKey,
	}

	return e.DoStreamRequest(ctx, "POST", url, headers, body)
}
