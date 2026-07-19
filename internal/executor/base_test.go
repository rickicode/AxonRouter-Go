package executor

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
