package executor

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const groundingResolveTimeout = 5 * time.Second

var (
	// antigravityGroundingCache memoizes resolved Vertex Search redirect URLs.
	antigravityGroundingCache sync.Map // string (redirect URL) -> string (target URL)

	// antigravityGroundingResolve performs the actual HEAD request. It is
	// overridable in tests to avoid real network calls.
	antigravityGroundingResolve = resolveAntigravityGroundingURL
)

// isAntigravityVertexSearchRedirect reports whether rawURL is a Vertex Search
// grounding redirect that should be resolved to its final target.
func isAntigravityVertexSearchRedirect(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.Scheme == "https" &&
		parsed.Host == "vertexaisearch.cloud.google.com" &&
		strings.HasPrefix(parsed.Path, "/grounding-api-redirect/")
}

// resolveAntigravityGroundingURL resolves a Vertex Search redirect URL to its
// final target using a HEAD request that does not follow redirects. If the
// resolution fails or the URL is not a redirect, rawURL is returned unchanged.
func resolveAntigravityGroundingURL(ctx context.Context, client *http.Client, rawURL string) string {
	if !isAntigravityVertexSearchRedirect(rawURL) {
		return rawURL
	}

	resolveCtx, cancel := context.WithTimeout(ctx, groundingResolveTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(resolveCtx, http.MethodHead, rawURL, nil)
	if err != nil {
		logging.Logger.Debug("antigravity grounding url: create request failed", "url", rawURL, "error", err)
		return rawURL
	}

	// Disable redirects so the server returns the Location header directly.
	resolveClient := &http.Client{
		Transport: client.Transport,
		Timeout:   groundingResolveTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := resolveClient.Do(req)
	if err != nil {
		logging.Logger.Debug("antigravity grounding url: resolve redirect failed", "url", rawURL, "error", err)
		return rawURL
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			logging.Logger.Debug("antigravity grounding url: close response failed", "url", rawURL, "error", err)
		}
	}()

	if resp.StatusCode < http.StatusMultipleChoices || resp.StatusCode >= http.StatusBadRequest {
		return rawURL
	}

	location := strings.TrimSpace(resp.Header.Get("Location"))
	if location == "" {
		return rawURL
	}

	parsed, err := url.Parse(location)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return rawURL
	}
	return location
}

// resolveAntigravityGroundingURLs replaces Vertex Search redirect URLs in
// grounding chunks with their cached resolved targets. URLs not yet in the
// cache are left as-is, and resolution is kicked off asynchronously so the
// current response is not blocked.
func resolveAntigravityGroundingURLs(ctx context.Context, client *http.Client, payload []byte) []byte {
	if len(payload) == 0 {
		return payload
	}

	basePath := "response.candidates.0.groundingMetadata.groundingChunks"
	chunks := gjson.GetBytes(payload, basePath)
	if !chunks.IsArray() {
		basePath = "candidates.0.groundingMetadata.groundingChunks"
		chunks = gjson.GetBytes(payload, basePath)
	}
	if !chunks.IsArray() {
		return payload
	}

	output := payload
	for i, chunk := range chunks.Array() {
		uri := strings.TrimSpace(chunk.Get("web.uri").String())
		if uri == "" {
			continue
		}

		resolvedURI, ok := antigravityGroundingCache.Load(uri)
		if !ok {
			// Not cached yet; start an async resolution without blocking.
			go resolveGroundingURLInBackground(uri, client)
			continue
		}

		if resolvedURI.(string) == uri {
			continue
		}
		updated, err := sjson.SetBytes(output, fmt.Sprintf("%s.%d.web.uri", basePath, i), resolvedURI)
		if err != nil {
			logging.Logger.Debug("antigravity grounding url: set resolved url failed", "url", uri, "error", err)
			continue
		}
		output = updated
	}
	return output
}

// resolveGroundingURLInBackground resolves a redirect URL and stores the
// result in the process-wide cache. It uses a detached context so the resolution
// can finish even after the request context is cancelled.
func resolveGroundingURLInBackground(rawURL string, client *http.Client) {
	bgCtx := context.WithoutCancel(context.Background())
	resolved := antigravityGroundingResolve(bgCtx, client, rawURL)
	antigravityGroundingCache.Store(rawURL, resolved)
}

// resolveGroundingURLsWithChannel wraps an upstream SSE chunk channel so that
// every chunk is processed for grounding redirects before being forwarded.
func (e *AntigravityExecutor) resolveGroundingURLsWithChannel(ctx context.Context, upstream <-chan StreamChunk) chan StreamChunk {
	out := make(chan StreamChunk)
	go func() {
		defer close(out)
		client := e.groundingHTTPClient(ctx)
		for chunk := range upstream {
			if chunk.Err == nil && len(chunk.Payload) > 0 {
				chunk.Payload = e.resolveGroundingURLsInSSELine(ctx, client, chunk.Payload)
			}
			out <- chunk
		}
	}()
	return out
}

// resolveGroundingURLsInSSELine resolves grounding redirects inside a single SSE
// data line. Non-data lines are returned unchanged.
func (e *AntigravityExecutor) resolveGroundingURLsInSSELine(ctx context.Context, client *http.Client, line []byte) []byte {
	trimmed := bytes.TrimSpace(line)
	if !bytes.HasPrefix(trimmed, []byte("data:")) {
		return line
	}
	payload := bytes.TrimSpace(bytes.TrimPrefix(trimmed, []byte("data:")))
	if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
		return line
	}
	resolved := resolveAntigravityGroundingURLs(ctx, client, payload)
	if bytes.Equal(resolved, payload) {
		return line
	}
	return append(append([]byte("data: "), resolved...), '\n', '\n')
}

// groundingHTTPClient returns an HTTP client suitable for HEAD resolution
// requests. It respects the executor's proxy configuration.
func (e *AntigravityExecutor) groundingHTTPClient(ctx context.Context) *http.Client {
	client, _, err := e.clientForContext(ctx, "https://vertexaisearch.cloud.google.com", map[string]string{})
	if err != nil {
		return e.Client
	}
	return client
}

// resolveGroundingInResponse rewrites grounding redirect URLs in a non-streaming
// Antigravity response body.
func (e *AntigravityExecutor) resolveGroundingInResponse(ctx context.Context, body []byte) []byte {
	client := e.groundingHTTPClient(ctx)
	return resolveAntigravityGroundingURLs(ctx, client, body)
}
