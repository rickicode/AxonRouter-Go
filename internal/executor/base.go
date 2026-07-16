package executor

import (
	"bufio"
	"bytes"
	"compress/gzip"
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
	FetchTimeoutMs         int // timeout for response headers, e.g. 90000
	StreamIdleTimeoutMs    int // timeout between chunks, e.g. 60000
	StreamReadinessTimeoutMs int // timeout for first chunk, e.g. 300000
	StallTimeoutMs         int // raw-byte stall timeout (0 = use idle timeout)
	HoldbackMs             int // holdback buffer window in ms (default 750)
	HoldbackBytes          int // holdback buffer max bytes (default 65536)
	AdaptiveReadiness      bool // enable adaptive readiness extension
}

// StallTapReader wraps an io.Reader and calls onBytes on every successful Read.
// ponytail: thin wrapper, no alloc per Read — the byte-count callback is a closure.
type StallTapReader struct {
	r       io.Reader
	onBytes func(n int)
}

func (s *StallTapReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 && s.onBytes != nil {
		s.onBytes(n)
	}
	return n, err
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

// TokenCounter is implemented by executors that can count tokens locally.
type TokenCounter interface {
	CountTokens(ctx context.Context, req *Request) (*Response, error)
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
	proxyContextKey   struct{}
	requestIDKey      struct{}
	providerCtxKey    struct{}
)

// ContextWithProvider attaches the provider prefix to ctx so the base executor
// can translate streaming upstream errors with the right translator.
func ContextWithProvider(ctx context.Context, provider string) context.Context {
	return context.WithValue(ctx, providerCtxKey{}, provider)
}

func providerFromContext(ctx context.Context) string {
	v, _ := ctx.Value(providerCtxKey{}).(string)
	return v
}

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
	return context.WithValue(ctx, proxyContextKey{}, cfg)
}

type proxyCandidatesKey struct{}

// ContextWithProxyCandidates attaches the ordered list of proxy configs to try.
// The executor retries across them on transient proxy/network failures. The
// first entry is the primary (already attached via ContextWithProxy).
func ContextWithProxyCandidates(ctx context.Context, cands []ProxyConfig) context.Context {
	return context.WithValue(ctx, proxyCandidatesKey{}, cands)
}

// ProxyCandidatesFromContext returns the retry candidate list, if any.
func ProxyCandidatesFromContext(ctx context.Context) []ProxyConfig {
	cands, _ := ctx.Value(proxyCandidatesKey{}).([]ProxyConfig)
	return cands
}

// isRetryableProxyErr reports whether err is a transient proxy/network failure
// worth retrying against another proxy. Upstream errors (4xx/5xx from the
// provider) and explicit cancellations are NOT retryable.
func isRetryableProxyErr(err error) bool {
	if err == nil {
		return false
	}
	var ue *UpstreamError
	if errors.As(err, &ue) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		// Deadline exceeded here is a stream idle/readiness timeout, not a proxy
		// connect failure; do not retry across proxies for it.
		return false
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	msg := err.Error()
	for _, s := range []string{
		"unexpected EOF",
		"connection reset by peer",
		"broken pipe",
		"connection refused",
		"no route to host",
		"dial ",
		"proxy dial",
		"proxy CONNECT",
		"proxy handshake",
		"TLS handshake",
		"tls",
		"i/o timeout",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
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

// DoRequest performs a non-streaming HTTP request, retrying across proxy
// candidates on transient proxy/network failures.
func (b *BaseExecutor) DoRequest(ctx context.Context, method, rawURL string, headers map[string]string, body []byte) (*Response, error) {
	if err := validateURL(rawURL); err != nil {
		return nil, fmt.Errorf("blocked URL: %w", err)
	}
	cands := ProxyCandidatesFromContext(ctx)
	if len(cands) <= 1 {
		return b.doRequestOnce(ctx, method, rawURL, headers, body)
	}
	var lastErr error
	for i, cand := range cands {
		attemptCtx := ContextWithProxy(ctx, cand)
		resp, err := b.doRequestOnce(attemptCtx, method, rawURL, headers, body)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isRetryableProxyErr(err) {
			return nil, err
		}
		logging.Logger.Warn("proxy attempt failed, retrying with next candidate", "attempt", i+1, "proxy", cand.ProxyLabel(), "error", err)
	}
	return nil, lastErr
}

// doRequestOnce performs a single non-streaming attempt using the proxy already
// attached to ctx.
func (b *BaseExecutor) doRequestOnce(ctx context.Context, method, rawURL string, headers map[string]string, body []byte) (*Response, error) {
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
	if req.Header.Get("Accept-Encoding") == "" {
		req.Header.Set("Accept-Encoding", "gzip")
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

	respBody, err := readResponseBody(resp.Body, resp.Header)
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

// DoStreamRequestWithConfig performs a streaming HTTP request with per-request
// timeout overrides, retrying across proxy candidates on transient
// proxy/network connect failures.
func (b *BaseExecutor) DoStreamRequestWithConfig(ctx context.Context, method, rawURL string, headers map[string]string, body []byte, cfg *StreamConfig) (*StreamResult, error) {
	if err := validateURL(rawURL); err != nil {
		return nil, fmt.Errorf("blocked URL: %w", err)
	}
	cands := ProxyCandidatesFromContext(ctx)
	if len(cands) <= 1 {
		return b.doStreamConnect(ctx, method, rawURL, headers, body, cfg)
	}
	var lastErr error
	for i, cand := range cands {
		attemptCtx := ContextWithProxy(ctx, cand)
		result, err := b.doStreamConnect(attemptCtx, method, rawURL, headers, body, cfg)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !isRetryableProxyErr(err) {
			return nil, err
		}
		logging.Logger.Warn("proxy stream attempt failed, retrying with next candidate", "attempt", i+1, "proxy", cand.ProxyLabel(), "error", err)
	}
	return nil, lastErr
}

// doStreamConnect opens a single streaming attempt using the proxy attached to
// ctx. It returns once the connection is established (or a connect error).
func (b *BaseExecutor) doStreamConnect(ctx context.Context, method, rawURL string, headers map[string]string, body []byte, cfg *StreamConfig) (*StreamResult, error) {
	// Use stream client (no global Timeout) for long-lived streaming responses.
	// Stream lifecycle is governed by fetch/idle/readiness context timeouts instead.
	client, targetURL, err := b.clientForContextStream(ctx, rawURL, headers)
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
	stallTimeout := b.StreamIdleTimeout // default: same as idle
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
		if cfg.StallTimeoutMs > 0 {
			stallTimeout = time.Duration(cfg.StallTimeoutMs) * time.Millisecond
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
	if req.Header.Get("Accept-Encoding") == "" {
		req.Header.Set("Accept-Encoding", "gzip")
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
		resp.Body = wrapMaybeGzipReader(resp.Body, resp.Header.Get("Content-Encoding"))
		errBody, _ := io.ReadAll(resp.Body)
		logging.Logger.Error(
			"upstream error response",
			"request_id", RequestIDFromContext(ctx),
			"status", resp.StatusCode,
			"host", logHost,
			"proxy", proxyLabelFromCtx(ctx),
			"body", string(errBody),
		)
		upErr := &UpstreamError{
			StatusCode: resp.StatusCode,
			Body:       errBody,
			RawBody:    errBody,
			Headers:    resp.Header,
		}
		if provider := providerFromContext(ctx); provider != "" {
			upErr.TranslateErrorBody(provider)
		}
		return nil, upErr
	}

	chunks := make(chan StreamChunk, 64)
	result := &StreamResult{
		Chunks:     chunks,
		Headers:    resp.Header,
		StatusCode: resp.StatusCode,
	}

	go func() {
		defer close(chunks)
		defer fetchCancel()
		resp.Body = wrapMaybeGzipReader(resp.Body, resp.Header.Get("Content-Encoding"))

		// Raw-byte stall detection: wrap body so every byte read resets the stall timer.
		// ponytail: StallTapReader is a thin io.Reader wrapper, no per-Read alloc.
		stallBytes := make(chan int, 64)
		tappedBody := &StallTapReader{r: resp.Body, onBytes: func(n int) {
			select {
			case stallBytes <- n:
			default: // non-blocking; channel is just a signal
			}
		}}
		defer resp.Body.Close()

		scanner := bufio.NewScanner(tappedBody)
		// ponytail: 64KB max line size, good enough for SSE
		scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

		// Run scanner in its own goroutine so select on idle timeout
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

		stallTimer := time.NewTimer(stallTimeout)
		stallTimer.Stop()
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
					stallTimer.Reset(stallTimeout)
				} else {
					if !idleTimer.Stop() {
						select {
						case <-idleTimer.C:
						default:
						}
					}
					idleTimer.Reset(idleTimeout)
					// Note: stall timer resets on raw bytes, not SSE lines.
					// It is reset in the stallBytes case below.
				}
				select {
				case chunks <- StreamChunk{Payload: line}:
				case <-ctx.Done():
					chunks <- StreamChunk{Err: ctx.Err()}
					return
				}

		case <-stallBytes:
			// Raw bytes arrived from upstream - reset stall timer.
			// This fires independently of SSE line boundaries so reasoning
			// models that send partial frames or keepalive pings do not
			// false-positive as stalled.
			// Skip before first chunk: the readiness timer governs that
			// window. Arming the stall timer early with a custom
			// StallTimeoutMs < StreamReadinessTimeoutMs would false-positive.
			if !sawFirst {
				continue
			}
			if !stallTimer.Stop() {
				select {
				case <-stallTimer.C:
				default:
				}
			}
			stallTimer.Reset(stallTimeout)

			case <-readinessTimer.C:
				chunks <- StreamChunk{Err: fmt.Errorf("stream readiness timeout after %v: %w", readinessTimeout, context.DeadlineExceeded)}
				return

			case <-idleTimer.C:
				chunks <- StreamChunk{Err: fmt.Errorf("stream idle timeout after %v: %w", idleTimeout, context.DeadlineExceeded)}
				return

			case <-stallTimer.C:
				// No raw bytes from upstream for stallTimeout - upstream is hung.
				chunks <- StreamChunk{Err: fmt.Errorf("stream stall timeout after %v: %w", stallTimeout, context.DeadlineExceeded)}
				return

			case <-ctx.Done():
				chunks <- StreamChunk{Err: ctx.Err()}
				return
			}
		}
	}()

	return result, nil
}

// WrapWithHoldback buffers initial stream chunks for a short window so the
// caller can retry transparently if an error arrives before any useful data
// reaches the client. Matches OmniRoute holdback buffer (750ms / 64KB).
//
// ctx controls goroutine lifetime: when ctx is cancelled the holdback is
// aborted cleanly (no goroutine leak on client disconnect).
//
// Returns:
//   - out: relay channel (buffered chunks + live relay)
//   - holdbackErr: receives a non-nil error if the stream failed during the
//     holdback window (caller should retry). Nil means holdback committed.
//
// ponytail: single goroutine, no extra alloc after the buffer slice.
func WrapWithHoldback(ctx context.Context, chunks chan StreamChunk, holdbackMs int, holdbackBytes int) (out chan StreamChunk, holdbackErr chan error) {
	out = make(chan StreamChunk, 64)
	errCh := make(chan error, 1)

	if holdbackMs <= 0 {
		holdbackMs = 750
	}
	if holdbackBytes <= 0 {
		holdbackBytes = 64 * 1024
	}

	go func() {
		defer close(out)

		var buf []StreamChunk
		bufBytes := 0
		holdbackTimer := time.NewTimer(time.Duration(holdbackMs) * time.Millisecond)
		defer holdbackTimer.Stop()

	// Phase 1: collect into buffer until timer fires or buffer is full.
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				// Stream ended during holdback — signal nil then flush buffer.
				// IMPORTANT: send to errCh FIRST so the caller unblocks and
				// starts reading out. Sending to out before errCh would
				// deadlock when buf exceeds out's channel buffer (64).
				errCh <- nil
				for _, c := range buf {
					out <- c
				}
				return
			}
			if chunk.Err != nil {
				// Error during holdback — signal caller to retry.
				errCh <- chunk.Err
				return
			}
			buf = append(buf, chunk)
			bufBytes += len(chunk.Payload)
			if bufBytes >= holdbackBytes {
				// Buffer full — commit immediately.
				goto commit
			}

		case <-holdbackTimer.C:
			goto commit

		case <-ctx.Done():
			// Client disconnect during holdback — abort cleanly.
			// The caller has already returned; closing out lets any
			// late reader see end-of-stream.
			select {
			case errCh <- nil: // best-effort signal in case caller still listening
			default:
			}
			return
		}
	}

commit:
	// Phase 2: flush buffer, then relay live chunks.
	errCh <- nil // holdback committed successfully
	for _, c := range buf {
		select {
		case out <- c:
		case <-ctx.Done():
			return
		}
	}
	for {
		select {
		case chunk, ok := <-chunks:
			if !ok {
				return
			}
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		case <-ctx.Done():
			return
		}
	}
	}()

	return out, errCh
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

// gzipMagic is the first two bytes of a gzip-encoded stream.
var gzipMagic = []byte{0x1f, 0x8b}

// readResponseBody reads the entire upstream body and transparently decompresses
// it if the response indicates gzip encoding or the body starts with the gzip
// magic bytes. This matches CLIProxyAPI's handling of upstreams that send gzip
// even when not explicitly requested.
func readResponseBody(r io.Reader, h http.Header) ([]byte, error) {
	if h.Get("Content-Encoding") == "gzip" {
		gr, err := gzip.NewReader(r)
		if err != nil {
			return nil, err
		}
		defer gr.Close()
		return io.ReadAll(gr)
	}
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if len(body) >= 2 && body[0] == gzipMagic[0] && body[1] == gzipMagic[1] {
		gr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			// Not a valid gzip stream after all; return the raw bytes.
			return body, nil
		}
		defer gr.Close()
		decompressed, err := io.ReadAll(gr)
		if err != nil {
			return body, nil
		}
		return decompressed, nil
	}
	return body, nil
}

// maybeGzipReadCloser wraps an io.ReadCloser and decompresses gzip streams when
// either the Content-Encoding header says gzip or the body starts with gzip magic.
type maybeGzipReadCloser struct {
	br         *bufio.Reader
	gr         *gzip.Reader
	underlying io.ReadCloser
}

func (m *maybeGzipReadCloser) Read(p []byte) (int, error) {
	if m.gr != nil {
		return m.gr.Read(p)
	}
	return m.br.Read(p)
}

func (m *maybeGzipReadCloser) Close() error {
	if m.gr != nil {
		m.gr.Close()
	}
	return m.underlying.Close()
}

// wrapMaybeGzipReader returns a reader that decompresses gzip if needed.
func wrapMaybeGzipReader(r io.ReadCloser, encoding string) io.ReadCloser {
	br := bufio.NewReader(r)
	if encoding == "gzip" {
		gr, err := gzip.NewReader(br)
		if err != nil {
			return &gzipErrorReader{err: err, underlying: r}
		}
		return &maybeGzipReadCloser{br: br, gr: gr, underlying: r}
	}
	head, err := br.Peek(2)
	if err == nil && len(head) >= 2 && head[0] == gzipMagic[0] && head[1] == gzipMagic[1] {
		gr, err := gzip.NewReader(br)
		if err != nil {
			return &gzipErrorReader{err: err, underlying: r}
		}
		return &maybeGzipReadCloser{br: br, gr: gr, underlying: r}
	}
	return &maybeGzipReadCloser{br: br, underlying: r}
}

type gzipErrorReader struct {
	err        error
	underlying io.ReadCloser
}

func (g *gzipErrorReader) Read([]byte) (int, error) { return 0, g.err }
func (g *gzipErrorReader) Close() error               { return g.underlying.Close() }
