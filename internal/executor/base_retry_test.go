package executor

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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
