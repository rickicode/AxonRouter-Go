package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// StreamChunk is a single SSE chunk from a streaming response.
type StreamChunk struct {
	Payload []byte
	Err     error
}

// StreamResult wraps a streaming response.
type StreamResult struct {
	Chunks     chan StreamChunk
	Headers    http.Header
	StatusCode int
}

// StreamConfig holds per-request streaming tunables.
type StreamConfig struct {
	FetchTimeoutMs           int // timeout for response headers, e.g. 90000
	StreamIdleTimeoutMs      int // timeout between chunks, e.g. 60000
	StreamReadinessTimeoutMs int // timeout for first chunk, e.g. 300000
}

// Response is a non-streaming response.
type Response struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
	Usage      map[string]int64 // optional provider-reported token usage
}

// Request is the unified execution request.
type Request struct {
	Model  string
	Body   []byte
	Stream bool
	// Connection credentials
	APIKey      string
	AccessToken string
	BaseURL     string
	Provider    string
	// Provider-specific data (e.g., projectId for Antigravity)
	ProviderSpecificData map[string]string
	// Extra headers
	Headers map[string]string
	// Streaming tunables (nil → use BaseExecutor defaults)
	StreamConfig *StreamConfig
}

// Executor executes requests against a provider.
type Executor interface {
	Execute(ctx context.Context, req *Request) (*Response, error)
	ExecuteStream(ctx context.Context, req *Request) (*StreamResult, error)
}

// BaseExecutor provides shared HTTP logic for all executors.
type BaseExecutor struct {
	Client                 *http.Client
	Timeout                time.Duration
	FetchTimeout           time.Duration
	StreamIdleTimeout      time.Duration
	StreamReadinessTimeout time.Duration
	proxyClients           sync.Map // proxyURL -> *http.Client (non-streaming)
	streamBase             *http.Client
	streamClients          sync.Map // proxyURL -> *http.Client (streaming, no Timeout)
}

// NewBaseExecutor creates a base executor with default settings.
// Timeout defaults match OmniRoute runtimeTimeouts.ts:
//
//	FETCH_TIMEOUT_MS=600000 (10m), STREAM_IDLE_TIMEOUT_MS=600000 (10m),
//	STREAM_READINESS_TIMEOUT_MS=80000 (80s).
func defaultHTTPTransport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:          1000,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
		DialContext:           defaultDialContext(),
	}
}

// defaultDialContext adds TCP keep-alive so dead proxy/upstream connections are
// detected sooner, and wraps dial errors with context for clearer logs.
func defaultDialContext() func(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := d.DialContext(ctx, network, addr)
		if err != nil {
			return nil, fmt.Errorf("dial %s %s failed: %w", network, addr, err)
		}
		return conn, nil
	}
}

func NewBaseExecutor() *BaseExecutor {
	b := &BaseExecutor{
		Client: &http.Client{
			Timeout:   5 * time.Minute,
			Transport: defaultHTTPTransport(),
		},
		// Streaming uses a client with no global Timeout; stream lifecycle is
		// governed by fetch/idle/readiness context timeouts instead.
		streamBase: &http.Client{
			Transport: defaultHTTPTransport(),
		},
		Timeout: 5 * time.Minute,
		FetchTimeout: time.Duration(getEnvInt("FETCH_TIMEOUT_MS", 600000)) * time.Millisecond,
		StreamIdleTimeout: time.Duration(getEnvInt("STREAM_IDLE_TIMEOUT_MS", 600000)) * time.Millisecond,
		StreamReadinessTimeout: time.Duration(getEnvInt("STREAM_READINESS_TIMEOUT_MS", 80000)) * time.Millisecond,
	}
	// Periodically drop idle connections so stale proxy/upstream sockets are
	// not reused after the peer silently closed them (common EOF source).
	go func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for range t.C {
			b.CloseIdleConnections()
		}
	}()
	return b
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

type (
	proxyContextKey struct{}
	requestIDKey    struct{}
)

// ContextWithRequestID attaches a request ID to a context for propagation
// to upstream providers.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFromContext extracts the request ID from a context.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// ProxyConfig is attached to request contexts by v1 handlers.
type ProxyConfig struct {
	Enabled     bool
	ProxyPoolID string
	ProxyURL    string
	NoProxy     string
	RelayURL    string
	RelayAuth   string
	RelayType   string
	StrictProxy bool
}

// ProxyLabel returns a human-readable proxy description for logging.
func (c ProxyConfig) ProxyLabel() string {
	if c.RelayURL != "" {
		host := c.RelayURL
		if parsed, err := url.Parse(c.RelayURL); err == nil && parsed.Host != "" {
			host = parsed.Host
		}
		return "relay/" + c.RelayType + " " + host
	}
	if c.ProxyURL != "" {
		return "http " + c.ProxyURL
	}
	return "direct"
}

// proxyLabelFromCtx extracts the proxy label from context for logging.
func proxyLabelFromCtx(ctx context.Context) string {
	if cfg, ok := ctx.Value(proxyContextKey{}).(ProxyConfig); ok {
		return cfg.ProxyLabel()
	}
	return "direct"
}

// ProxyPoolIDFromContext extracts the proxy pool ID from context for logging.
func ProxyPoolIDFromContext(ctx context.Context) string {
	if cfg, ok := ctx.Value(proxyContextKey{}).(ProxyConfig); ok {
		return cfg.ProxyPoolID
	}
	return ""
}

func ContextWithProxy(ctx context.Context, cfg ProxyConfig) context.Context {
	if !cfg.Enabled && cfg.RelayURL == "" {
		return ctx
	}
	return context.WithValue(ctx, proxyContextKey{}, cfg)
}

// validateURL checks for SSRF-safe URLs. Blocks private IPs and localhost.
// Defined as a var so tests can override it.
var validateURL = func(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}

	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "0.0.0.0" {
		return fmt.Errorf("localhost not allowed")
	}
	if strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return fmt.Errorf("local/internal hostname not allowed")
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

// resolveTargetURL applies relay rewriting if proxy config has a relay URL.
func resolveTargetURL(rawURL string, cfg ProxyConfig) (string, map[string]string) {
	extra := map[string]string{}
	if cfg.RelayURL == "" {
		return rawURL, extra
	}
	target, _ := url.Parse(rawURL)
	extra["x-relay-target"] = target.Scheme + "://" + target.Host
	extra["x-relay-path"] = target.Path
	if target.RawQuery != "" {
		extra["x-relay-path"] = target.Path + "?" + target.RawQuery
	}
	if cfg.RelayAuth != "" {
		extra["x-relay-auth"] = cfg.RelayAuth
	}
	return cfg.RelayURL, extra
}

// proxyClient returns an http.Client that routes through the given proxy URL.
// ponytail: cached per proxy URL, avoids creating a new transport per request.
func (b *BaseExecutor) proxyClient(proxyURL string) (*http.Client, error) {
	if v, ok := b.proxyClients.Load(proxyURL); ok {
		return v.(*http.Client), nil
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}
	transport := defaultHTTPTransport()
	transport.Proxy = http.ProxyURL(u)
	// HTTP/2 over an HTTP CONNECT proxy is flaky across providers; keep it off
	// for proxied traffic to avoid mid-stream EOFs.
	transport.ForceAttemptHTTP2 = false
	c := &http.Client{Timeout: b.Timeout, Transport: transport}
	b.proxyClients.Store(proxyURL, c)
	return c, nil
}

// streamClient returns an http.Client for streaming through the given proxy.
// It has no global Timeout so long-lived SSE streams are not cut at 5 minutes;
// stream timeouts are enforced via context instead.
func (b *BaseExecutor) streamClient(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		return b.streamBase, nil
	}
	if v, ok := b.streamClients.Load(proxyURL); ok {
		return v.(*http.Client), nil
	}
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}
	transport := defaultHTTPTransport()
	transport.Proxy = http.ProxyURL(u)
	transport.ForceAttemptHTTP2 = false
	c := &http.Client{Transport: transport}
	b.streamClients.Store(proxyURL, c)
	return c, nil
}

// CloseIdleConnections drops idle keep-alive connections for the default and
// all cached proxy clients. Call when proxy pool state changes so stale
// connections to a removed/disabled proxy are not reused.
func (b *BaseExecutor) CloseIdleConnections() {
	if t, ok := b.Client.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
	if t, ok := b.streamBase.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
	b.proxyClients.Range(func(_, v any) bool {
		if t, ok := v.(*http.Client).Transport.(*http.Transport); ok {
			t.CloseIdleConnections()
		}
		return true
	})
	b.streamClients.Range(func(_, v any) bool {
		if t, ok := v.(*http.Client).Transport.(*http.Transport); ok {
			t.CloseIdleConnections()
		}
		return true
	})
}

// noProxyMatch checks if a hostname matches any entry in a comma-separated no_proxy list.
// Matches exact host or suffix (e.g. "example.com" matches ".example.com").
func noProxyMatch(host, noProxy string) bool {
	if noProxy == "" {
		return false
	}
	host = strings.ToLower(host)
	for _, entry := range strings.Split(noProxy, ",") {
		entry = strings.TrimSpace(strings.ToLower(entry))
		if entry == "" {
			continue
		}
		if host == entry {
			return true
		}
		if strings.HasPrefix(entry, ".") && (strings.HasSuffix(host, entry) || host == entry[1:]) {
			return true
		}
	}
	return false
}

// clientForContext picks the right http.Client and target URL for a request.
// Returns error only when StrictProxy is true and proxy is unavailable.
func (b *BaseExecutor) clientForContext(ctx context.Context, rawURL string, headers map[string]string) (*http.Client, string, error) {
	return b.selectClient(ctx, rawURL, headers, false)
}

// clientForContextStream is like clientForContext but uses clients without a
// global Timeout, suitable for long-lived streaming responses.
func (b *BaseExecutor) clientForContextStream(ctx context.Context, rawURL string, headers map[string]string) (*http.Client, string, error) {
	return b.selectClient(ctx, rawURL, headers, true)
}

func (b *BaseExecutor) selectClient(ctx context.Context, rawURL string, headers map[string]string, stream bool) (*http.Client, string, error) {
	cfg, _ := ctx.Value(proxyContextKey{}).(ProxyConfig)
	targetURL, extra := resolveTargetURL(rawURL, cfg)
	for k, v := range extra {
		headers[k] = v
	}

	// Relay: always use default client (URL already rewritten)
	if cfg.RelayURL != "" {
		return b.defaultClient(stream), targetURL, nil
	}

	// No proxy configured
	if cfg.ProxyURL == "" {
		return b.defaultClient(stream), targetURL, nil
	}

	// Check noProxy: skip proxy for matching hosts
	if cfg.NoProxy != "" {
		u, err := url.Parse(targetURL)
		if err == nil && noProxyMatch(u.Hostname(), cfg.NoProxy) {
			return b.defaultClient(stream), targetURL, nil
		}
	}

	// Get proxy client
	var (
		c   *http.Client
		err error
	)
	if stream {
		c, err = b.streamClient(cfg.ProxyURL)
	} else {
		c, err = b.proxyClient(cfg.ProxyURL)
	}
	if err != nil {
		if cfg.StrictProxy {
			return nil, targetURL, fmt.Errorf("strict proxy unavailable: %w", err)
		}
		// Non-strict: fall back to direct
		return b.defaultClient(stream), targetURL, nil
	}
	return c, targetURL, nil
}

func (b *BaseExecutor) defaultClient(stream bool) *http.Client {
	if stream {
		return b.streamBase
	}
	return b.Client
}

// DoRequest performs a non-streaming HTTP request.
func (b *BaseExecutor) DoRequest(ctx context.Context, method, rawURL string, headers map[string]string, body []byte) (*Response, error) {
	if err := validateURL(rawURL); err != nil {
		return nil, fmt.Errorf("blocked URL: %w", err)
	}

	client, targetURL, err := b.clientForContextStream(ctx, rawURL, headers)
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" && body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if id := RequestIDFromContext(ctx); id != "" {
		req.Header.Set("X-Request-ID", id)
	}

	logging.Logger.Info(
		"upstream request start",
		"request_id", RequestIDFromContext(ctx),
		"method", method,
		"url", targetURL,
		"proxy", proxyLabelFromCtx(ctx),
	)

	resp, err := client.Do(req)
	if err != nil {
		logging.Logger.Warn(
			"upstream request failed",
			"request_id", RequestIDFromContext(ctx),
			"method", method,
			"url", targetURL,
			"proxy", proxyLabelFromCtx(ctx),
			"error", err,
		)
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		logging.Logger.Error(
			"upstream error response",
			"request_id", RequestIDFromContext(ctx),
			"status", resp.StatusCode,
			"url", targetURL,
			"proxy", proxyLabelFromCtx(ctx),
			"body", string(respBody),
		)
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       respBody,
	}, nil
}

// DoStreamRequest performs a streaming HTTP request and returns chunks via channel.
func (b *BaseExecutor) DoStreamRequest(ctx context.Context, method, rawURL string, headers map[string]string, body []byte) (*StreamResult, error) {
	return b.DoStreamRequestWithConfig(ctx, method, rawURL, headers, body, nil)
}

// DoStreamRequestWithConfig performs a streaming HTTP request with per-request timeout overrides.
func (b *BaseExecutor) DoStreamRequestWithConfig(ctx context.Context, method, rawURL string, headers map[string]string, body []byte, cfg *StreamConfig) (*StreamResult, error) {
	if err := validateURL(rawURL); err != nil {
		return nil, fmt.Errorf("blocked URL: %w", err)
	}

	client, targetURL, err := b.clientForContext(ctx, rawURL, headers)
	if err != nil {
		return nil, err
	}

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	// Determine effective timeouts
	fetchTimeout := b.FetchTimeout
	idleTimeout := b.StreamIdleTimeout
	readinessTimeout := b.StreamReadinessTimeout
	if cfg != nil {
		if cfg.FetchTimeoutMs > 0 {
			fetchTimeout = time.Duration(cfg.FetchTimeoutMs) * time.Millisecond
		}
		if cfg.StreamIdleTimeoutMs > 0 {
			idleTimeout = time.Duration(cfg.StreamIdleTimeoutMs) * time.Millisecond
		}
		if cfg.StreamReadinessTimeoutMs > 0 {
			readinessTimeout = time.Duration(cfg.StreamReadinessTimeoutMs) * time.Millisecond
		}
	}

	// Fetch timeout covers until response headers arrive.
	fetchCtx, fetchCancel := context.WithTimeout(ctx, fetchTimeout)

	req, err := http.NewRequestWithContext(fetchCtx, method, targetURL, bodyReader)
	if err != nil {
		fetchCancel()
		return nil, fmt.Errorf("create request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" && body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if id := RequestIDFromContext(ctx); id != "" {
		req.Header.Set("X-Request-ID", id)
	}

	logHost := targetURL
	if parsed, err := url.Parse(targetURL); err == nil && parsed.Host != "" {
		logHost = parsed.Host
	}
	logging.Logger.Info(
		"upstream stream request start",
		"request_id", RequestIDFromContext(ctx),
		"method", method,
		"host", logHost,
		"proxy", proxyLabelFromCtx(ctx),
	)

	resp, err := client.Do(req)
	if err != nil {
		fetchCancel()
		logHost := targetURL
		if parsed, err := url.Parse(targetURL); err == nil && parsed.Host != "" {
			logHost = parsed.Host
		}
		logging.Logger.Warn(
			"upstream stream request failed",
			"request_id", RequestIDFromContext(ctx),
			"method", method,
			"host", logHost,
			"proxy", proxyLabelFromCtx(ctx),
			"error", err,
		)
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("stream fetch timeout (%v): %w", fetchTimeout, err)
		}
		return nil, fmt.Errorf("do request: %w", err)
	}

	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		defer fetchCancel()
		errBody, _ := io.ReadAll(resp.Body)
		logging.Logger.Error(
			"upstream error response",
			"request_id", RequestIDFromContext(ctx),
			"status", resp.StatusCode,
			"host", logHost,
			"proxy", proxyLabelFromCtx(ctx),
			"body", string(errBody),
		)
		return nil, &UpstreamError{
			StatusCode: resp.StatusCode,
			Body:       errBody,
			RawBody:    errBody,
			Headers:    resp.Header,
		}
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
		defer fetchCancel()

		scanner := bufio.NewScanner(resp.Body)
		// ponytail: 64KB max line size, good enough for SSE
		scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

		// Run scanner in its own goroutine so we can select on idle timeout
		scanCh := make(chan []byte, 1)
		scanErrCh := make(chan error, 1)
		go func() {
			defer close(scanCh)
			defer close(scanErrCh)
			for scanner.Scan() {
				line := append([]byte{}, scanner.Bytes()...)
				select {
				case scanCh <- line:
				case <-ctx.Done():
					return
				}
			}
			if err := scanner.Err(); err != nil {
				select {
				case scanErrCh <- err:
				case <-ctx.Done():
				}
			}
		}()

		readinessTimer := time.NewTimer(readinessTimeout)
		defer readinessTimer.Stop()

		idleTimer := time.NewTimer(idleTimeout)
		idleTimer.Stop()
		var sawFirst bool

		for {
			select {
			case line, ok := <-scanCh:
				if !ok {
					if err := <-scanErrCh; err != nil {
						chunks <- StreamChunk{Err: err}
					}
					return
				}
				if len(line) == 0 {
					continue
				}
				if !sawFirst {
					sawFirst = true
					if !readinessTimer.Stop() {
						select {
						case <-readinessTimer.C:
						default:
						}
					}
					idleTimer.Reset(idleTimeout)
				} else {
					if !idleTimer.Stop() {
						select {
						case <-idleTimer.C:
						default:
						}
					}
					idleTimer.Reset(idleTimeout)
				}
				select {
				case chunks <- StreamChunk{Payload: line}:
				case <-ctx.Done():
					chunks <- StreamChunk{Err: ctx.Err()}
					return
				}

			case <-readinessTimer.C:
				chunks <- StreamChunk{Err: fmt.Errorf("stream readiness timeout after %v: %w", readinessTimeout, context.DeadlineExceeded)}
				return

			case <-idleTimer.C:
				chunks <- StreamChunk{Err: fmt.Errorf("stream idle timeout after %v: %w", idleTimeout, context.DeadlineExceeded)}
				return

			case <-ctx.Done():
				chunks <- StreamChunk{Err: ctx.Err()}
				return
			}
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

// WriteSSEHeartbeat writes a synthetic OpenAI-style heartbeat chunk.
// Matches OmniRoute OPENAI_CHUNK shape (sseHeartbeat.ts buildHeartbeatPayload).
func WriteSSEHeartbeat(w io.Writer, flusher http.Flusher) {
	payload := map[string]any{
		"id":      "axonrouter-keepalive",
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   "axonrouter",
		"choices": []map[string]any{
			{"index": 0, "delta": map[string]any{}, "finish_reason": nil},
		},
	}
	b, _ := json.Marshal(payload)
	fmt.Fprintf(w, "data: %s\n\n", b)
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
		return strings.TrimPrefix(model[:idx], "@")
	}
	return ""
}

// JSONSet is a helper to set a field in raw JSON.
func JSONSet(data []byte, path string, value any) []byte {
	out, err := sjson.SetBytes(data, path, value)
	if err != nil {
		return data
	}
	return out
}

// JSONGet extracts a string field from raw JSON.
func JSONGet(data []byte, path string) string {
	return gjson.GetBytes(data, path).String()
}

// IsStreamRequest checks if the body requests streaming.
func IsStreamRequest(body []byte) bool {
	r := gjson.GetBytes(body, "stream")
	if r.Type == gjson.String {
		b, _ := strconv.ParseBool(r.String())
		return b
	}
	return r.Bool()
}

// JSONToReader wraps a byte slice as an io.Reader.
func JSONToReader(data []byte) io.Reader {
	return bytes.NewReader(data)
}
