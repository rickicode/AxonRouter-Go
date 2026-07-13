package executor

import (
	"net/http"
	"testing"
)

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
