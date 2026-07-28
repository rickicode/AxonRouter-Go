package headroom

import (
	"context"
	"strings"
	"testing"
)

func TestProcessManager(t *testing.T) {
	pm := NewProcessManager("/usr/bin/false")
	if pm.Running() {
		t.Fatal("expected not running")
	}
	err := pm.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not yet implemented") {
		t.Fatalf("expected not-implemented error, got %v", err)
	}
	if err := pm.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
}
