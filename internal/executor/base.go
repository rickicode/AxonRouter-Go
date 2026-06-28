package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// StreamChunk is a single SSE chunk from a streaming response.
type StreamChunk struct {
	Payload []byte
	Err     error
}

// StreamResult wraps a streaming response.
type StreamResult struct {
	Chunks    chan StreamChunk
	Headers   http.Header
	StatusCode int
}

// Response is a non-streaming response.
type Response struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

// Request is the unified execution request.
type Request struct {
	Model    string
	Body     []byte
	Stream   bool
	// Connection credentials
	APIKey      string
	AccessToken string
	BaseURL     string
	Provider    string
	// Extra headers
	Headers map[string]string
}

// Executor executes requests against a provider.
type Executor interface {
	Execute(ctx context.Context, req *Request) (*Response, error)
	ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error)
}

// BaseExecutor provides shared HTTP logic for all executors.
type BaseExecutor struct {
	Client  *http.Client
	Timeout time.Duration
}

// NewBaseExecutor creates a base executor with default settings.
func NewBaseExecutor() *BaseExecutor {
	return &BaseExecutor{
		Client:  &http.Client{Timeout: 5 * time.Minute},
		Timeout: 5 * time.Minute,
	}
}

// validateURL checks for SSRF-safe URLs. Blocks private IPs and localhost.
func validateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}

	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return fmt.Errorf("localhost not allowed")
	}

	ip := net.ParseIP(host)
	if ip != nil {
		if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("private IP not allowed: %s", host)
		}
		// Block cloud metadata endpoints
		if host == "169.254.169.254" || host == "fd00:ec2::254" {
			return fmt.Errorf("metadata endpoint not allowed")
		}
	}

	return nil
}

// DoRequest performs a non-streaming HTTP request.
func (b *BaseExecutor) DoRequest(ctx context.Context, method, url string, headers map[string]string, body []byte) (*Response, error) {
	if err := validateURL(url); err != nil {
		return nil, fmt.Errorf("blocked URL: %w", err)
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" && body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := b.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       respBody,
	}, nil
}

// DoStreamRequest performs a streaming HTTP request and returns chunks via channel.
func (b *BaseExecutor) DoStreamRequest(ctx context.Context, method, url string, headers map[string]string, body []byte) (*StreamResult, error) {
	if err := validateURL(url); err != nil {
		return nil, fmt.Errorf("blocked URL: %w", err)
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" && body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := b.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("stream error %d: %s", resp.StatusCode, string(errBody))
	}

	chunks := make(chan StreamChunk, 64)
	result := &StreamResult{
		Chunks:     chunks,
		Headers:    resp.Header,
		StatusCode: resp.StatusCode,
	}

	go func() {
		defer close(chunks)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		// ponytail: 64KB max line size, good enough for SSE
		scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			// Pass through full SSE lines (data: ...)
			select {
			case chunks <- StreamChunk{Payload: append([]byte{}, line...)}:
			case <-ctx.Done():
				chunks <- StreamChunk{Err: ctx.Err()}
				return
			}
		}

		if err := scanner.Err(); err != nil {
			chunks <- StreamChunk{Err: err}
		}
	}()

	return result, nil
}

// WriteSSE writes an SSE event to the response writer.
func WriteSSE(w io.Writer, flusher http.Flusher, data []byte) {
	fmt.Fprintf(w, "data: %s\n\n", data)
	if flusher != nil {
		flusher.Flush()
	}
}

// WriteSSEDone writes the [DONE] marker.
func WriteSSEDone(w io.Writer, flusher http.Flusher) {
	fmt.Fprintf(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

// SetAuthHeader sets the appropriate auth header based on available credentials.
func SetAuthHeader(headers map[string]string, apiKey, accessToken string) {
	if accessToken != "" {
		headers["Authorization"] = "Bearer " + accessToken
	} else if apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}
}

// ExtractModel strips provider prefix from model name.
// "openai/gpt-4o" → "gpt-4o", "gpt-4o" → "gpt-4o"
func ExtractModel(model string) string {
	if idx := strings.Index(model, "/"); idx >= 0 {
		return model[idx+1:]
	}
	return model
}

// ExtractProvider extracts provider prefix from model name.
// "openai/gpt-4o" → "openai", "gpt-4o" → ""
func ExtractProvider(model string) string {
	if idx := strings.Index(model, "/"); idx >= 0 {
		return model[:idx]
	}
	return ""
}

// JSONSet is a helper to set a field in raw JSON.
func JSONSet(data []byte, path string, value any) []byte {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return data
	}
	m[path] = value
	out, _ := json.Marshal(m)
	return out
}

// JSONGet extracts a string field from raw JSON.
func JSONGet(data []byte, path string) string {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	if v, ok := m[path]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// IsStreamRequest checks if the body requests streaming.
func IsStreamRequest(body []byte) bool {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return false
	}
	v, ok := m["stream"]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

// JSONToReader wraps a byte slice as an io.Reader.
func JSONToReader(data []byte) io.Reader {
	return bytes.NewReader(data)
}
