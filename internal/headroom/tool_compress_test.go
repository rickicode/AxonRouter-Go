package headroom

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompressToolBlocks_Disabled(t *testing.T) {
	tc := NewToolCompressor(Config{Enabled: false})
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"tool_result","content":"` + strings.Repeat("x", 1000) + `"}]}]}`)
	out := tc.CompressToolBlocks(body, 100)
	if string(out) != string(body) {
		t.Fatalf("expected unchanged body when disabled")
	}
}

func TestCompressToolBlocks_UnderThreshold(t *testing.T) {
	tc := NewToolCompressor(Config{Enabled: true})
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"tool_result","content":"short"}]}]}`)
	out := tc.CompressToolBlocks(body, 100)
	if string(out) != string(body) {
		t.Fatalf("expected unchanged body under threshold")
	}
}

func TestCompressToolBlocks_UnknownKind(t *testing.T) {
	tc := NewToolCompressor(Config{Enabled: true})
	text := "random plain text with no discernible structure " + strings.Repeat("x", 500)
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"tool_result","content":"` + text + `"}]}]}`)
	out := tc.CompressToolBlocks(body, 100)
	if string(out) != string(body) {
		t.Fatalf("expected unchanged body for unknown kind")
	}
}

func TestCompressToolBlocks_GitDiff(t *testing.T) {
	tc := NewToolCompressor(Config{Enabled: true})
	diff := sampleGitDiff()
	msg := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "tool_result", "content": diff},
				},
			},
		},
	}
	body, _ := json.Marshal(msg)
	out := tc.CompressToolBlocks(body, 50)
	if string(out) == string(body) {
		t.Fatalf("expected body to be compressed")
	}
	dOut := NewDefaultDetector().Detect([]byte(diff))
	if dOut != KindGitDiff {
		t.Fatalf("detector returned %q, want git_diff", dOut)
	}
}
