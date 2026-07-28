package thrm

import (
	"os"
	"strings"
	"testing"

	"github.com/rickicode/AxonRouter-Go/internal/headroom"
)

func TestCompressToolText_Disabled(t *testing.T) {
	_ = os.Setenv("AXON_HEADROOM_ENABLED", "false")
	defer os.Unsetenv("AXON_HEADROOM_ENABLED")
	SetClient(headroom.NewClient(headroom.DefaultConfig(), nil, nil, nil))
	in := []byte(strings.Repeat("a very long tool output ", 50))
	out := CompressToolText(in)
	if string(out) != string(in) {
		t.Fatalf("expected unchanged output when disabled")
	}
}

func TestCompressToolText_Small(t *testing.T) {
	_ = os.Setenv("AXON_HEADROOM_ENABLED", "true")
	defer os.Unsetenv("AXON_HEADROOM_ENABLED")
	SetClient(headroom.NewClient(headroom.DefaultConfig(), nil, nil, nil))
	in := []byte("small")
	out := CompressToolText(in)
	if string(out) != string(in) {
		t.Fatalf("expected unchanged output for small payload")
	}
}

func TestCompressToolText_Compresses(t *testing.T) {
	_ = os.Setenv("AXON_HEADROOM_ENABLED", "true")
	defer os.Unsetenv("AXON_HEADROOM_ENABLED")

	compressor := &headroom.DefaultCompressor{}
	SetClient(headroom.NewClient(headroom.DefaultConfig(), headroom.NewDefaultDetector(), compressor, nil))
	in := []byte(strings.Repeat("diff --git a/file1 b/file1\n--- a/file1\n+++ b/file1\n@@ -1 +1 @@\n-old\n+new\n", 30))
	out := CompressToolText(in)
	if len(out) >= len(in) {
		t.Fatalf("expected compression to reduce size: %d >= %d", len(out), len(in))
	}
}
