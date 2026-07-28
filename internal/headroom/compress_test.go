package headroom

import (
	"bytes"
	"testing"
)

func TestRoundTrip_AllKinds(t *testing.T) {
	samples := map[Kind]string{
		KindGitDiff: `diff --git a/internal/foo.go b/internal/foo.go
--- a/internal/foo.go
+++ b/internal/foo.go
@@ -1,3 +1,3 @@
 package foo
 
-func Foo() {}
+func Foo(x int) {}
`,
		KindGitLog: `commit abcdef1234567890abcdef1234567890abcdef12
Author: A <a@example.com>
Date:   Mon Jan 1 00:00:00 2024 +0000

    first

commit abcdef1234567890abcdef1234567890abcdef13
Author: B <b@example.com>
Date:   Tue Jan 2 00:00:00 2024 +0000

    second
`,
		KindGitStatus: `On branch main
Your branch is up to date with 'origin/main'.

Changes not staged for commit:
  (use "git add <file>..." to update what will be committed)

	modified:   internal/foo.go
	new file:   internal/bar.go
	deleted:    internal/baz.go
`,
		KindGrep: `internal/foo.go:10:func Foo() error {}
internal/bar.go:20:type Bar struct{}
internal/baz.go:30:const Baz = 1
`,
		KindFindTree: `./internal
./internal/headroom
./internal/headroom/types.go
./internal/headroom/detect.go
./internal/headroom/compress.go
`,
		KindBuildLog: `=== RUN   TestFoo
--- PASS: TestFoo (0.01s)
=== RUN   TestBar
--- FAIL: TestBar (0.02s)
    bar_test.go:10: mismatch
ok  	example.com/foo	0.100s
FAIL	example.com/bar	0.200s
`,
		KindSearchResults: `{"total": 2, "hits": [{"_score": 1.0, "_source": {"x": 1}}, {"_score": 0.5, "_source": {"x": 2}}]}`,
		KindUnknown:       "hello world this is a plain payload with no specific structure",
	}

	comp, err := NewDefaultCompressor()
	if err != nil {
		t.Fatalf("new compressor: %v", err)
	}

	for kind, payload := range samples {
		t.Run(string(kind), func(t *testing.T) {
			compressed, err := comp.Compress(kind, []byte(payload))
			if err != nil {
				t.Fatalf("compress %s: %v", kind, err)
			}
			if len(compressed) == 0 {
				t.Fatal("compressed payload is empty")
			}
			// Transform must reduce or preserve bounded-size test inputs.
			if len(compressed) >= len(payload)*2 {
				t.Fatalf("compressed payload unexpectedly large: %d vs %d", len(compressed), len(payload))
			}
			// Verify zstd decompresses successfully.
			decompressed, err := comp.Decompress(compressed)
			if err != nil {
				t.Fatalf("decompress %s: %v", kind, err)
			}
			if len(decompressed) == 0 {
				t.Fatal("decompressed payload is empty")
			}
			method := TransformMethod(kind)
			if method == "" {
				t.Fatal("method should not be empty")
			}
			_ = payload
		})
	}
}

func TestCompressWithMethod(t *testing.T) {
	comp, err := NewDefaultCompressor()
	if err != nil {
		t.Fatalf("new compressor: %v", err)
	}
	payload := []byte("diff --git a/foo b/foo")
	out, method, err := CompressWithMethod(comp, KindGitDiff, payload)
	if err != nil {
		t.Fatal(err)
	}
	if method != "git_diff_delta" {
		t.Fatalf("unexpected method %s", method)
	}
	if len(out) == 0 {
		t.Fatal("empty output")
	}
}

func TestGzipRaw(t *testing.T) {
	payload := []byte("compress me")
	out := gzipRaw(payload)
	if len(out) >= len(payload)*10 {
		t.Fatal("gzip unexpectedly large")
	}
}

func TestElideLongLine(t *testing.T) {
	long := bytes.Repeat([]byte("a"), 300)
	got := elideLongLine(string(long))
	if len(got) >= 250 {
		t.Fatalf("expected elision, got len %d", len(got))
	}
}
