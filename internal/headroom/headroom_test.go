package headroom

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	// Ensure defaults are populated without relying on current env.
	cfg := DefaultConfig()
	if cfg.Endpoint != DefaultEndpoint {
		t.Fatalf("endpoint default: got %q want %q", cfg.Endpoint, DefaultEndpoint)
	}
	if cfg.TimeoutMs != DefaultTimeoutMs {
		t.Fatalf("timeout default: got %d want %d", cfg.TimeoutMs, DefaultTimeoutMs)
	}
	if cfg.MaxPayloadBytes != DefaultMaxPayloadBytes {
		t.Fatalf("max payload default: got %d want %d", cfg.MaxPayloadBytes, DefaultMaxPayloadBytes)
	}
	if cfg.Enabled {
		t.Fatal("headroom should be disabled by default")
	}
}

func TestDetectAllKinds(t *testing.T) {
	d := NewDefaultDetector()
	cases := []struct {
		name string
		data string
		want Kind
	}{
		{
			name: "git_diff",
			data: "diff --git a/internal/foo.go b/internal/foo.go\n--- a/internal/foo.go\n+++ b/internal/foo.go\n@@ -1,3 +1,3 @@\n line1\n-line2\n+line2modified\n",
			want: KindGitDiff,
		},
		{
			name: "git_log",
			data: "commit abc123\nAuthor: Test User <test@example.com>\nDate:   Mon Jan 1 00:00:00 2024 +0000\n\n    initial commit",
			want: KindGitLog,
		},
		{
			name: "git_status",
			data: "On branch main\nYour branch is up to date.\n\nChanges to be committed:\n  (use git restore --staged <file>... to unstage)\n\n\tmodified:   file.go\n",
			want: KindGitStatus,
		},
		{
			name: "grep",
			data: "internal/foo.go:10:func main() {\ninternal/foo.go:11:\tfmt.Println(\"hello\")\n",
			want: KindGrep,
		},
		{
			name: "find_tree",
			data: "./internal/headroom/types.go\n./internal/headroom/detect.go\n./internal/headroom/compress.go\n",
			want: KindFindTree,
		},
		{
			name: "build_log",
			data: "[INFO] Building target\n[ERROR] something failed\n[WARNING] deprecated call\n",
			want: KindBuildLog,
		},
		{
			name: "search_results",
			data: "Results for query\n1. First result title\n2. Second result title\n",
			want: KindSearchResults,
		},
		{
			name: "unknown",
			data: "random plain text with no discernible structure",
			want: KindUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := d.Detect([]byte(tc.data))
			if got != tc.want {
				t.Errorf("Detect(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestCompressRoundTrip(t *testing.T) {
	c := NewDefaultCompressor()
	cases := []struct {
		name string
		data string
		kind Kind
	}{
		{"git_diff", sampleGitDiff(), KindGitDiff},
		{"git_log", sampleGitLog(), KindGitLog},
		{"git_status", sampleGitStatus(), KindGitStatus},
		{"grep", sampleGrep(), KindGrep},
		{"find_tree", sampleFindTree(), KindFindTree},
		{"build_log", sampleBuildLog(), KindBuildLog},
		{"search_results", sampleSearchResults(), KindSearchResults},
		{"unknown", "plain text\n\nwith   spaces", KindUnknown},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := c.Compress([]byte(tc.data), tc.kind)
			if err != nil {
				t.Fatalf("Compress error: %v", err)
			}
			if len(out) == 0 {
				t.Fatal("compressed output is empty")
			}
			// Round-trip sanity: compressed output should be no larger than input.
			if len(out) > len(tc.data) {
				t.Fatalf("compressed output larger than input: %d > %d", len(out), len(tc.data))
			}
		})
	}
}

func TestServiceCompressEndpoint(t *testing.T) {
	cfg := Config{
		Enabled:         true,
		Endpoint:        "127.0.0.1:19123",
		TimeoutMs:       5000,
		MaxPayloadBytes: 524288,
	}
	metrics := NewMetrics()
	svc := NewService(cfg, nil, nil, metrics)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = svc.ListenAndServe()
	}()
	t.Cleanup(func() {
		_ = svc.Close()
		wg.Wait()
	})

	waitForAddr(t, cfg.Endpoint)

	in := Input{Data: []byte(sampleGitDiff())}
	body, _ := json.Marshal(in)
	resp, err := http.Post("http://"+cfg.Endpoint+"/compress", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var out Output
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Kind != KindGitDiff {
		t.Fatalf("kind: got %q want %q", out.Kind, KindGitDiff)
	}
	if out.CompressedBytes > out.OriginalBytes {
		t.Fatal("compression did not reduce size")
	}
	if metrics.Total() != 1 {
		t.Fatalf("metrics total: got %d want 1", metrics.Total())
	}
}

func TestClientFallback(t *testing.T) {
	cfg := Config{
		Enabled:         true,
		Endpoint:        "127.0.0.1:19124",
		TimeoutMs:       100,
		MaxPayloadBytes: 524288,
	}
	client := NewClient(cfg, nil, nil, NewMetrics())
	// Endpoint is down, fallback should work.
	out, err := client.Compress([]byte(sampleGitLog()))
	if err != nil {
		t.Fatalf("compress error: %v", err)
	}
	if out.Kind != KindGitLog {
		t.Fatalf("kind: got %q want %q", out.Kind, KindGitLog)
	}
	if out.CompressedBytes > out.OriginalBytes {
		t.Fatal("fallback compression did not reduce size")
	}
}

func TestProcessLifecycle(t *testing.T) {
	cfg := Config{
		Enabled:         true,
		Endpoint:        "127.0.0.1:19125",
		TimeoutMs:       2000,
		MaxPayloadBytes: 524288,
	}
	proc := NewProcess(cfg, nil, nil, NewMetrics())
	if err := proc.Start(); err != nil {
		t.Fatalf("start error: %v", err)
	}
	if !proc.Running() {
		t.Fatal("process should be running")
	}

	waitForAddr(t, cfg.Endpoint)

	in := Input{Data: []byte(sampleGitStatus())}
	body, _ := json.Marshal(in)
	resp, err := http.Post("http://"+cfg.Endpoint+"/compress", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	if err := proc.Stop(); err != nil {
		t.Fatalf("stop error: %v", err)
	}
	if proc.Running() {
		t.Fatal("process should not be running")
	}
}

func TestMetricsRecord(t *testing.T) {
	m := NewMetrics()
	m.RecordSuccess(1000, 700)
	m.RecordError()
	if m.Total() != 2 {
		t.Fatalf("total: got %d want 2", m.Total())
	}
	if m.BytesSaved() != 300 {
		t.Fatalf("bytes saved: got %d want 300", m.BytesSaved())
	}
	if m.Errors() != 1 {
		t.Fatalf("errors: got %d want 1", m.Errors())
	}
}

func waitForAddr(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/health")
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("service never became ready at %s", addr)
}

func sampleGitDiff() string {
	return strings.TrimSpace(`
diff --git a/internal/foo.go b/internal/foo.go
index 1234567..abcdefg 100644
--- a/internal/foo.go
+++ b/internal/foo.go
@@ -1,5 +1,5 @@
 package foo
 
 func Foo() string {
-    return "old"
+    return "new"
 }
`) + "\n"
}

func sampleGitLog() string {
	return strings.TrimSpace(`
commit abcdef1234567890
Author: Test User <test@example.com>
Date:   Mon Jan 1 00:00:00 2024 +0000

    initial commit

    Long description line that should be preserved.
`) + "\n"
}

func sampleGitStatus() string {
	return strings.TrimSpace(`
On branch main
Your branch is up to date with 'origin/main'.

Changes to be committed:
  (use "git restore --staged <file>..." to unstage)
	modified:   internal/headroom/types.go
	modified:   internal/headroom/detect.go
`) + "\n"
}

func sampleGrep() string {
	return strings.TrimSpace(`
internal/foo.go:10:func Foo() string {
internal/foo.go:11:    return "bar"
internal/foo.go:12:}
`) + "\n"
}

func sampleFindTree() string {
	return strings.TrimSpace(`
./internal/headroom/types.go
./internal/headroom/detect.go
./internal/headroom/compress.go
./internal/headroom/service.go
`) + "\n"
}

func sampleBuildLog() string {
	return strings.TrimSpace(`
[INFO] Building project
[WARNING] deprecated API usage
[INFO] progress [=====>    ] 50%
[ERROR] test failure in package foo
`) + "\n"
}

func sampleSearchResults() string {
	return strings.TrimSpace(`
Results for "headroom"
1. Headroom - Wikipedia https://en.wikipedia.org/wiki/Headroom
2. Headroom (audio) overview https://example.com/audio
Some long description text that might be truncated because it exceeds the allowed line length for search result summaries.
`) + "\n"
}
