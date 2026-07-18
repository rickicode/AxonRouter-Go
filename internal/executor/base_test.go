package executor

import (
	"context"
	"net/http"
	"testing"
)

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
