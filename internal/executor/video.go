package executor

import (
	"context"
	"fmt"
)

// VideoExecutor handles video generation requests.
type VideoExecutor struct {
	*BaseExecutor
}

// NewVideoExecutor creates a new video executor.
func NewVideoExecutor(base *BaseExecutor) *VideoExecutor {
	return &VideoExecutor{BaseExecutor: base}
}

// Execute performs a video generation request.
func (e *VideoExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://api.openai.com/v1/video/generations"
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
		return nil, fmt.Errorf("video error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return resp, nil
}

// ExecuteStream is not supported for video generation.
func (e *VideoExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	return nil, fmt.Errorf("video generation does not support streaming")
}
