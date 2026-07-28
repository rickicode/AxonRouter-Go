package headroom

import (
	"context"
	"testing"
	"time"
)

func TestClient_Disabled(t *testing.T) {
	cfg := Config{Enabled: false, MaxPayloadBytes: 1024, Timeout: 5 * time.Second}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if c.Enabled() {
		t.Fatal("expected disabled")
	}
	in := Input{Payload: []byte("hello world")}
	out, err := c.Compress(context.Background(), in)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	if out.Kind != KindUnknown {
		t.Fatalf("expected unknown, got %s", out.Kind)
	}
}

func TestClient_Enabled(t *testing.T) {
	cfg := Config{Enabled: true, MaxPayloadBytes: 4096, Timeout: 5 * time.Second}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if !c.Enabled() {
		t.Fatal("expected enabled")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.StartService(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer c.StopService(context.Background())

	in := Input{Payload: []byte("diff --git a/foo b/foo\n")}
	out, err := c.Compress(context.Background(), in)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	if out.Kind != KindGitDiff {
		t.Fatalf("expected git_diff, got %s", out.Kind)
	}
}

func TestClient_LocalCompress(t *testing.T) {
	cfg := Config{Enabled: false, MaxPayloadBytes: 1024, Timeout: 5 * time.Second}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	in := Input{Payload: []byte("ERROR: build failed\nWARNING: deprecation")}
	out, err := c.LocalCompress(context.Background(), in)
	if err != nil {
		t.Fatalf("local compress: %v", err)
	}
	if out.Kind != KindBuildLog {
		t.Fatalf("expected build_log, got %s", out.Kind)
	}
}
