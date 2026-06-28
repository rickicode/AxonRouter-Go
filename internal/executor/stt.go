package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// STTExecutor handles speech-to-text requests.
type STTExecutor struct {
	*BaseExecutor
}

// NewSTTExecutor creates a new STT executor.
func NewSTTExecutor(base *BaseExecutor) *STTExecutor {
	return &STTExecutor{BaseExecutor: base}
}

// Execute performs an STT request. Body should be multipart form data.
func (e *STTExecutor) Execute(ctx context.Context, req *Request) (*Response, error) {
	url := e.resolveURL(req)
	if err := validateURL(url); err != nil {
		return nil, fmt.Errorf("blocked URL: %w", err)
	}

	// For STT, the body is already multipart form data from the client
	// or we need to forward it as-is
	headers := map[string]string{
		"Authorization": "Bearer " + req.APIKey,
	}
	if req.AccessToken != "" {
		headers["Authorization"] = "Bearer " + req.AccessToken
	}

	// Use raw body forwarding for multipart
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(req.Body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}
	// Preserve original content type (multipart boundary)
	if ct := req.Headers["Content-Type"]; ct != "" {
		httpReq.Header.Set("Content-Type", ct)
	}

	httpResp, err := e.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode >= 400 {
		return nil, fmt.Errorf("stt error %d: %s", httpResp.StatusCode, string(body))
	}

	return &Response{
		StatusCode: httpResp.StatusCode,
		Headers:    httpResp.Header,
		Body:       body,
	}, nil
}

// ExecuteStream is not supported for STT.
func (e *STTExecutor) ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error) {
	return nil, fmt.Errorf("STT does not support streaming")
}

// BuildMultipartBody creates a multipart form body for STT requests.
func BuildMultipartBody(audioData []byte, filename, model, language string) ([]byte, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(audioData); err != nil {
		return nil, "", err
	}

	if model != "" {
		writer.WriteField("model", model)
	}
	if language != "" {
		writer.WriteField("language", language)
	}

	writer.Close()
	return buf.Bytes(), writer.FormDataContentType(), nil
}

func (e *STTExecutor) resolveURL(req *Request) string {
	if req.BaseURL != "" {
		return req.BaseURL
	}
	switch req.Provider {
	case "deepgram":
		return "https://api.deepgram.com/v1/listen"
	default:
		return "https://api.openai.com/v1/audio/transcriptions"
	}
}
