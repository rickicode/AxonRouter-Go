package headroom

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestInternalService_Compress(t *testing.T) {
	cfg := Config{Enabled: true, MaxPayloadBytes: 1024, Timeout: 5 * time.Second}
	svc, err := NewInternalService(cfg)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	in := Input{Payload: []byte("diff --git a/foo b/foo\n--- a/foo\n+++ b/foo\n")}
	out, err := svc.Compress(context.Background(), in)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	if out.Kind != KindGitDiff {
		t.Fatalf("expected git_diff, got %s", out.Kind)
	}
	if out.Method != "git_diff_delta" {
		t.Fatalf("expected git_diff_delta, got %s", out.Method)
	}
	if out.OriginalSize <= out.CompressedSize {
		t.Logf("original %d compressed %d", out.OriginalSize, out.CompressedSize)
	}
	if total, saved, errs := svc.Metrics().Snapshot(); total != 1 || saved < 0 || errs != 0 {
		t.Fatalf("unexpected metrics: %d %d %d", total, saved, errs)
	}
}

func TestInternalService_HTTP(t *testing.T) {
	cfg := Config{Enabled: true, MaxPayloadBytes: 4096, Timeout: 5 * time.Second}
	svc, err := NewInternalService(cfg)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop(context.Background())

	if err := InternalHealthCheck("http://"+svc.Addr(), 2*time.Second); err != nil {
		t.Fatalf("health: %v", err)
	}

	in := Input{Payload: []byte("On branch main\n\tmodified:   foo.go\n")}
	body, _ := json.Marshal(in)
	resp, err := http.Post("http://"+svc.Addr()+"/compress", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out Output
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Kind != KindGitStatus {
		t.Fatalf("expected git_status, got %s", out.Kind)
	}
}

func TestCompressViaHTTP(t *testing.T) {
	cfg := Config{Enabled: true, MaxPayloadBytes: 4096, Timeout: 5 * time.Second}
	svc, err := NewInternalService(cfg)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer svc.Stop(context.Background())

	if err := InternalHealthCheck("http://"+svc.Addr(), 2*time.Second); err != nil {
		t.Fatalf("health: %v", err)
	}

	in := Input{Payload: []byte("grep hit one\nfile.go:10:match here\nfile.go:20:match again\n")}
	out, err := CompressViaHTTP(context.Background(), "http://"+svc.Addr(), in)
	if err != nil {
		t.Fatalf("via http: %v", err)
	}
	if out.Kind != KindGrep {
		t.Fatalf("expected grep, got %s", out.Kind)
	}
}

func TestInternalService_PayloadTooLarge(t *testing.T) {
	cfg := Config{Enabled: true, MaxPayloadBytes: 10, Timeout: 5 * time.Second}
	svc, err := NewInternalService(cfg)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	in := Input{Payload: []byte("this payload is way too long")}
	_, err = svc.Compress(context.Background(), in)
	if err == nil {
		t.Fatal("expected error for oversized payload")
	}
}
