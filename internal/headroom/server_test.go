package headroom

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServerStartStop(t *testing.T) {
	s := NewServer()
	endpoint, err := s.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if endpoint == "" {
		t.Fatal("expected endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestServerCompressEndpoint(t *testing.T) {
	s := NewServer()
	endpoint, err := s.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Stop(context.Background())

	original := "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n@@ -1,5 +1,5 @@\n package main\n unchanged line one\n unchanged line two\n-hello\n+world\n"
	req := PayloadHeader{Original: original}
	data, _ := json.Marshal(req)
	resp, err := http.Post(endpoint+"/compress", "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var result CompressedResult
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Kind != KindGitDiff {
		t.Errorf("kind = %q, want %q", result.Kind, KindGitDiff)
	}
	if result.SavedBytes <= 0 {
		t.Errorf("expected positive savings, got %d; compressed=%q", result.SavedBytes, result.Compressed)
	}
}

func TestClientRoundTrip(t *testing.T) {
	s := NewServer()
	endpoint, err := s.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Stop(context.Background())

	client, err := NewClient(Config{Enabled: true, Endpoint: endpoint, TimeoutMs: 5000})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	text := "ok  github.com/rickicode/AxonRouter-Go/internal/headroom  0.006s\n\n\n"
	compressed, err := client.Compress(context.Background(), KindBuildLog, text)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	if len(compressed) >= len(text) {
		t.Errorf("expected compression, got %q", compressed)
	}
	if !strings.Contains(compressed, "ok") {
		t.Errorf("expected 'ok' retained in compressed output: %q", compressed)
	}
}

func TestClientDisabled(t *testing.T) {
	client, err := NewClient(Config{Enabled: false})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	text := "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go"
	out, err := client.Compress(context.Background(), KindGitDiff, text)
	if err != nil {
		t.Fatalf("compress disabled: %v", err)
	}
	if out != text {
		t.Errorf("disabled client should return original: got %q", out)
	}
}
