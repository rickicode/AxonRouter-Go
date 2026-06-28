package executor

import (
	"context"
	"fmt"
)

// ImagesExecutor handles image generation requests.
type ImagesExecutor struct {
	*BaseExecutor
}

// NewImagesExecutor creates a new images executor.
func NewImagesExecutor(base *BaseExecutor) *ImagesExecutor {
	return &ImagesExecutor{BaseExecutor: base}
}

// Execute performs an image generation request.
func (e *ImagesExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := req.BaseURL
	if url == "" {
		url = "https://api.openai.com/v1/images/generations"
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
		return nil, fmt.Errorf("images error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return resp, nil
}

// ExecuteStream is not supported for image generation.
func (e *ImagesExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	return nil, fmt.Errorf("image generation does not support streaming")
}
