package executor

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsRetryableProxyErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"eof", io.EOF, true},
		{"unexpected-eof", io.ErrUnexpectedEOF, true},
		{"op-error-dial", &net.OpError{Op: "dial", Err: errors.New("refused")}, true},
		{"op-error-read", &net.OpError{Op: "read", Err: errors.New("reset")}, true},
		{"msg-unexpected-eof", errors.New("unexpected EOF"), true},
		{"msg-conn-reset", errors.New("read: connection reset by peer"), true},
		{"msg-broken-pipe", errors.New("write: broken pipe"), true},
		{"msg-conn-refused", errors.New("dial tcp: connection refused"), true},
		{"msg-tls", errors.New("tls: failed to verify certificate: proxy handshake"), true},
		{"upstream-error", &UpstreamError{StatusCode: 500}, false},
		{"context-canceled", context.Canceled, false},
		{"plain", errors.New("some random error"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isRetryableProxyErr(c.err); got != c.want {
				t.Fatalf("isRetryableProxyErr(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

func TestDoRequestRetriesAcrossCandidates(t *testing.T) {
	orig := validateURL
	validateURL = func(string) error { return nil }
	defer func() { validateURL = orig }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()

	// First candidate points at a closed port (connection refused -> retryable),
	// second candidate is direct to the test server.
	cands := []ProxyConfig{
		{Enabled: true, ProxyURL: "http://127.0.0.1:1"},
		{Enabled: false},
	}
	ctx := ContextWithProxy(context.Background(), cands[0])
	ctx = ContextWithProxyCandidates(ctx, cands)

	base := NewBaseExecutor()
	resp, err := base.DoRequest(ctx, "GET", server.URL, map[string]string{}, []byte(""))
	if err != nil {
		t.Fatalf("DoRequest returned error: %v", err)
	}
	if string(resp.Body) != "ok" {
		t.Fatalf("unexpected body: %q", string(resp.Body))
	}
}

// TestSelectClientStrictNoProxyErrors guards the strict-proxy contract: a
// request marked StrictProxy with NO usable egress (empty ProxyURL/RelayURL)
// must fail instead of silently going direct. This is the anti-leak guarantee
// for Freebuff — the v1 handler force-sets StrictProxy on the direct-fallback
// candidate when all configured pools are dead, and that candidate must never
// claim the session from the gateway's real IP.
func TestSelectClientStrictNoProxyErrors(t *testing.T) {
	base := NewBaseExecutor()

	t.Run("strict-no-proxy-errors", func(t *testing.T) {
		ctx := ContextWithProxy(context.Background(), ProxyConfig{StrictProxy: true})
		_, _, err := base.clientForContext(ctx, "https://www.codebuff.com/api/v1/chat/completions", map[string]string{})
		if err == nil {
			t.Fatal("strict proxy with no egress must error, not go direct")
		}
		if !strings.Contains(err.Error(), "strict proxy required") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("non-strict-no-proxy-direct-ok", func(t *testing.T) {
		ctx := ContextWithProxy(context.Background(), ProxyConfig{})
		client, _, err := base.clientForContext(ctx, "https://www.codebuff.com/api/v1/chat/completions", map[string]string{})
		if err != nil {
			t.Fatalf("non-strict direct must be allowed: %v", err)
		}
		if client != base.Client {
			t.Errorf("expected the default direct client")
		}
	})

	t.Run("strict-with-proxy-ok", func(t *testing.T) {
		ctx := ContextWithProxy(context.Background(), ProxyConfig{ProxyURL: "http://127.0.0.1:1", StrictProxy: true})
		if _, _, err := base.clientForContext(ctx, "https://www.codebuff.com/api/v1/chat/completions", map[string]string{}); err != nil {
			t.Fatalf("strict proxy with a ProxyURL must not error: %v", err)
		}
	})

	t.Run("strict-relay-ok", func(t *testing.T) {
		// A relay IS a usable egress (URL-rewrite proxy) — strict is satisfied.
		ctx := ContextWithProxy(context.Background(), ProxyConfig{RelayURL: "https://relay.example.com", StrictProxy: true})
		if _, _, err := base.clientForContext(ctx, "https://www.codebuff.com/api/v1/chat/completions", map[string]string{}); err != nil {
			t.Fatalf("strict relay must not error: %v", err)
		}
	})
}

func TestDoRequestNoRetryOnUpstreamError(t *testing.T) {
	orig := validateURL
	validateURL = func(string) error { return nil }
	defer func() { validateURL = orig }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, "bad gateway")
	}))
	defer server.Close()

	// Single direct candidate; a 502 is an upstream error, not a proxy failure,
	// so DoRequest must NOT retry (there is nothing to retry against).
	cands := []ProxyConfig{{Enabled: false}}
	ctx := ContextWithProxy(context.Background(), cands[0])
	ctx = ContextWithProxyCandidates(ctx, cands)

	base := NewBaseExecutor()
	resp, err := base.DoRequest(ctx, "GET", server.URL, map[string]string{}, []byte(""))
	if err != nil {
		t.Fatalf("DoRequest returned error: %v", err)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}
}
