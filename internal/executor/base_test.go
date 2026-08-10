package executor

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/logging"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func captureExecutorLogs(t *testing.T, run func()) string {
	t.Helper()
	previous := logging.Logger.Load()
	var buf bytes.Buffer
	logging.SetLogger(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { logging.SetLogger(previous) })
	run()
	return buf.String()
}

func contextWithClientLogInfo() context.Context {
	ctx := context.Background()
	ctx = ContextWithClientIP(ctx, "203.0.113.10")
	ctx = ContextWithUserAgent(ctx, "axon-test/1.0")
	return ctx
}

func requireLogContainsClientInfo(t *testing.T, logs, message string) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		if strings.Contains(line, `"msg":"`+message+`"`) {
			if !strings.Contains(line, `"client_ip":"203.0.113.10"`) {
				t.Fatalf("log %q missing client_ip: %s", message, line)
			}
			if !strings.Contains(line, `"user_agent":"axon-test/1.0"`) {
				t.Fatalf("log %q missing user_agent: %s", message, line)
			}
			return
		}
	}
	t.Fatalf("missing log message %q in logs:\n%s", message, logs)
}

func TestClientIPFromContext(t *testing.T) {
	ctx := context.Background()
	if got := ClientIPFromContext(ctx); got != "" {
		t.Errorf("ClientIPFromContext(empty) = %q, want empty", got)
	}
	ctx = ContextWithClientIP(ctx, "192.168.1.1")
	if got := ClientIPFromContext(ctx); got != "192.168.1.1" {
		t.Errorf("ClientIPFromContext = %q, want 192.168.1.1", got)
	}
}

func TestUserAgentFromContext(t *testing.T) {
	ctx := context.Background()
	if got := UserAgentFromContext(ctx); got != "" {
		t.Errorf("UserAgentFromContext(empty) = %q, want empty", got)
	}
	ctx = ContextWithUserAgent(ctx, "Mozilla/5.0")
	if got := UserAgentFromContext(ctx); got != "Mozilla/5.0" {
		t.Errorf("UserAgentFromContext = %q, want Mozilla/5.0", got)
	}
}

func TestDoRequestLogsClientInfoForStartAndErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream failed", http.StatusBadGateway)
	}))
	defer server.Close()

	exec := &BaseExecutor{Client: server.Client(), streamBase: server.Client()}
	logs := captureExecutorLogs(t, func() {
		_, err := exec.DoRequest(contextWithClientLogInfo(), http.MethodPost, server.URL, map[string]string{}, []byte(`{}`))
		if err != nil {
			t.Fatalf("DoRequest returned error: %v", err)
		}
	})

	requireLogContainsClientInfo(t, logs, "upstream request start")
	requireLogContainsClientInfo(t, logs, "upstream error response")
}

func TestProxyConfigBuildsURLWithCredentials(t *testing.T) {
	cfg := ProxyConfig{ProxyURL: "http://proxy.example:8080", ProxyUsername: "user1", ProxyPassword: "pass1"}
	got := cfg.proxyURLWithCredentials()
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse built URL: %v", err)
	}
	if u.User == nil || u.User.Username() != "user1" {
		t.Fatalf("expected username user1, got %v", u.User)
	}
	if pass, _ := u.User.Password(); pass != "pass1" {
		t.Fatalf("expected password pass1, got %s", pass)
	}
	if u.Host != "proxy.example:8080" {
		t.Fatalf("expected host unchanged, got %s", u.Host)
	}
}

func TestProxyConfigCanonicalURLStripsCredentials(t *testing.T) {
	cfg := ProxyConfig{ProxyURL: "http://user:pass@proxy.example:8080"}
	if got := cfg.canonicalProxyURL(); got != "http://proxy.example:8080" {
		t.Fatalf("expected canonical URL without credentials, got %s", got)
	}
}

func TestProxyCacheKeyChangesWithCredentials(t *testing.T) {
	cfgA := ProxyConfig{ProxyURL: "http://proxy.example:8080", ProxyUsername: "user-a", ProxyPassword: "pass-a"}
	cfgB := ProxyConfig{ProxyURL: "http://proxy.example:8080", ProxyUsername: "user-b", ProxyPassword: "pass-b"}
	if cfgA.proxyCacheKey() == cfgB.proxyCacheKey() {
		t.Fatalf("expected different cache keys for different credentials")
	}
}

func TestProxyCacheKeyChangesWithInlineCredentials(t *testing.T) {
	cfgA := ProxyConfig{ProxyURL: "http://user-a:pass-a@proxy.example:8080"}
	cfgB := ProxyConfig{ProxyURL: "http://user-b:pass-b@proxy.example:8080"}
	if cfgA.proxyCacheKey() == cfgB.proxyCacheKey() {
		t.Fatalf("expected different cache keys for different inline credentials")
	}
}

func TestProxyClientCachesPerCredentialVersion(t *testing.T) {
	exec := NewBaseExecutor()
	cfgA := ProxyConfig{ProxyURL: "http://proxy.example:8080", ProxyUsername: "user-a", ProxyPassword: "pass-a"}
	cfgB := ProxyConfig{ProxyURL: "http://proxy.example:8080", ProxyUsername: "user-b", ProxyPassword: "pass-b"}
	clientA, err := exec.proxyClient(cfgA)
	if err != nil {
		t.Fatalf("proxyClient A: %v", err)
	}
	clientB, err := exec.proxyClient(cfgB)
	if err != nil {
		t.Fatalf("proxyClient B: %v", err)
	}
	if clientA == clientB {
		t.Fatalf("expected different clients for different credentials")
	}
	clientA2, err := exec.proxyClient(cfgA)
	if err != nil {
		t.Fatalf("proxyClient A2: %v", err)
	}
	if clientA != clientA2 {
		t.Fatalf("expected same cached client for identical config")
	}
}

func TestDoRequestLogsClientInfoForRequestFailure(t *testing.T) {
	expectedErr := errors.New("transport down")
	exec := &BaseExecutor{
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, expectedErr
		})},
		streamBase: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, expectedErr
		})},
	}

	logs := captureExecutorLogs(t, func() {
		_, err := exec.DoRequest(contextWithClientLogInfo(), http.MethodPost, "https://example.com/v1/chat", map[string]string{}, []byte(`{}`))
		if err == nil {
			t.Fatal("DoRequest returned nil error, want transport error")
		}
	})

	requireLogContainsClientInfo(t, logs, "upstream request start")
	requireLogContainsClientInfo(t, logs, "upstream request failed")
}

func TestNewBaseExecutorTransport(t *testing.T) {
	be := NewBaseExecutor()
	if be.Client == nil {
		t.Fatal("expected non-nil http.Client")
	}
	tr, ok := be.Client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", be.Client.Transport)
	}
	if tr.MaxIdleConns != 1000 {
		t.Errorf("MaxIdleConns = %d, want 1000", tr.MaxIdleConns)
	}
	if tr.MaxIdleConnsPerHost != 100 {
		t.Errorf("MaxIdleConnsPerHost = %d, want 100", tr.MaxIdleConnsPerHost)
	}
	if tr.IdleConnTimeout == 0 {
		t.Error("IdleConnTimeout should be set")
	}
	if !tr.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2 should be true")
	}
}

func TestProxyAuthHeaderForInlineCredentials(t *testing.T) {
	cfg := ProxyConfig{ProxyURL: "http://alice:secret@proxy.example:8080"}
	got := cfg.proxyAuthHeader()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:secret"))
	if got != want {
		t.Fatalf("proxyAuthHeader() = %q, want %q", got, want)
	}
}

func TestProxyAuthHeaderForSeparateFields(t *testing.T) {
	cfg := ProxyConfig{ProxyURL: "http://proxy.example:8080", ProxyUsername: "bob", ProxyPassword: "hunter2"}
	got := cfg.proxyAuthHeader()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("bob:hunter2"))
	if got != want {
		t.Fatalf("proxyAuthHeader() = %q, want %q", got, want)
	}
}

func TestNoProxyMatch(t *testing.T) {
	if !noProxyMatch("api.cloudflare.com", "cloudflare.com,localhost") {
		t.Fatal("should match suffix/descendant without leading dot")
	}
	if !noProxyMatch("api.cloudflare.com", ".cloudflare.com,localhost") {
		t.Fatal("should match suffix with leading dot")
	}
	if !noProxyMatch("cloudflare.com", ".cloudflare.com") {
		t.Fatal("should match root domain against leading-dot rule")
	}
	if !noProxyMatch("localhost", "localhost") {
		t.Fatal("should match exact hostname")
	}
	if noProxyMatch("other.com", "cloudflare.com") {
		t.Fatal("should not match unrelated host")
	}
	if noProxyMatch("subnotcloudflare.com", "cloudflare.com") {
		t.Fatal("should not match unrelated suffix host")
	}
}

func TestProxyClientBypassesProxyForNoProxy(t *testing.T) {
	exec := NewBaseExecutor()
	cfg := ProxyConfig{ProxyURL: "http://proxy.example:8080", NoProxy: "cloudflare.com"}
	// The client should still be a proxy client (not the default), because only
	// requests to non-matching hosts go through the proxy. The transport itself
	// is configured to skip the proxy at request time.
	client, err := exec.proxyClient(cfg)
	if err != nil {
		t.Fatalf("proxyClient: %v", err)
	}
	tr := client.Transport.(*http.Transport)
	// Request to cloudflare.com should bypass proxy.
	req, _ := http.NewRequest("GET", "https://api.cloudflare.com/v1", nil)
	u, err := tr.Proxy(req)
	if err != nil {
		t.Fatalf("Proxy: %v", err)
	}
	if u != nil {
		t.Fatalf("expected proxy bypass for api.cloudflare.com, got %v", u)
	}
	// Request to example.com should use proxy.
	req2, _ := http.NewRequest("GET", "https://example.com/v1", nil)
	u2, err := tr.Proxy(req2)
	if err != nil {
		t.Fatalf("Proxy: %v", err)
	}
	if u2 == nil {
		t.Fatal("expected proxy to be used for example.com")
	}
}

func TestProxyConnectHeaderSetExplicitly(t *testing.T) {
	exec := NewBaseExecutor()
	cfg := ProxyConfig{ProxyURL: "http://user:pass@proxy.example:8080"}
	client, err := exec.proxyClient(cfg)
	if err != nil {
		t.Fatalf("proxyClient: %v", err)
	}
	tr := client.Transport.(*http.Transport)
	got := tr.ProxyConnectHeader.Get("Proxy-Authorization")
	want := cfg.proxyAuthHeader()
	if got != want || got == "" {
		t.Fatalf("ProxyConnectHeader Proxy-Authorization = %q, want non-empty %q", got, want)
	}
}

func TestProxyStreamClientConnectHeaderSetExplicitly(t *testing.T) {
	exec := NewBaseExecutor()
	cfg := ProxyConfig{ProxyURL: "http://suser:spass@proxy.example:8080"}
	client, err := exec.streamClient(cfg)
	if err != nil {
		t.Fatalf("streamClient: %v", err)
	}
	tr := client.Transport.(*http.Transport)
	got := tr.ProxyConnectHeader.Get("Proxy-Authorization")
	want := cfg.proxyAuthHeader()
	if got != want || got == "" {
		t.Fatalf("streamClient ProxyConnectHeader Proxy-Authorization = %q, want non-empty %q", got, want)
	}
}

func TestProxyClientCacheInvalidatedOnVersionChange(t *testing.T) {
	exec := NewBaseExecutor()
	cfgA := ProxyConfig{ProxyURL: "http://proxy.example:8080", ProxyUsername: "user", ProxyPassword: "old", Version: "v1"}
	cfgB := cfgA
	cfgB.Version = "v2"

	clientA, err := exec.proxyClient(cfgA)
	if err != nil {
		t.Fatalf("proxyClient v1: %v", err)
	}
	clientB, err := exec.proxyClient(cfgB)
	if err != nil {
		t.Fatalf("proxyClient v2: %v", err)
	}
	if clientA == clientB {
		t.Fatal("expected different clients after version change")
	}
}
