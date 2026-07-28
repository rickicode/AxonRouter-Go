package headroom

import (
	"strings"
	"testing"
)

func TestDetectPayloadKind(t *testing.T) {
	cases := []struct {
		name string
		text string
		want PayloadKind
	}{
		{"empty", "", KindGeneric},
		{"git diff", "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n@@ -1,5 +1,5 @@\n context", KindGitDiff},
		{"git log", "commit abcdef1234567890abcdef1234567890abcdef12\nAuthor: A <a@example.com>\nDate: Mon Jan 1 00:00:00 2024\n\n    summary\n\ncommit 1111111111111111111111111111111111111111\nAuthor: B <b@example.com>", KindGitLog},
		{"git status", " M foo.go\n?? bar.go", KindGitStatus},
		{"grep", "foo.go:42:hello\nfoo.go:43:world", KindGrep},
		{"find tree", "a/b.go\na/c.go\nd.go\ne/f.go", KindFindTree},
		{"build log", "[INFO] Build started\nFAIL: test\nerror: bad", KindBuildLog},
		{"search", "1. Result one\n2. Result two\nPath: /tmp", KindSearch},
		{"generic", "hello world", KindGeneric},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectPayloadKind(tc.text)
			if got != tc.want {
				t.Errorf("DetectPayloadKind() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDetectPayloadKindGitStatusNotDiff(t *testing.T) {
	// A diff should not be classified as git_status even though it contains paths.
	text := "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n M foo.go"
	if got := DetectPayloadKind(text); got != KindGitDiff {
		t.Errorf("expected %q for diff containing status-like line, got %q", KindGitDiff, got)
	}
}

func TestCompressGitDiff(t *testing.T) {
	diff := "diff --git a/foo.go b/foo.go\nindex 123..456 789\n--- a/foo.go\n+++ b/foo.go\n@@ -1,5 +1,5 @@\n package foo\n context\n some unchanged line that is especially long and should be truncated because it is plain unchanged context padding text and continues for a while\n-removed\n+added\n"
	out, saved, tech := Compress(KindGitDiff, diff)
	if len(out) >= len(diff) {
		t.Errorf("expected compression, got saved=%d", saved)
	}
	if saved <= 0 {
		t.Errorf("expected positive savings, got %d", saved)
	}
	if !contains(tech, "git_diff_context_trim") {
		t.Errorf("expected git_diff_context_trim technique, got %v", tech)
	}
}

func TestCompressBuildLog(t *testing.T) {
	log := "\x1b[32m[00:00:01]\x1b[0m build start\n\n\nb\n\n\nok  test passed"
	out, saved, tech := Compress(KindBuildLog, log)
	if saved <= 0 {
		t.Errorf("expected positive savings, got %d", saved)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("ANSI escapes should be stripped: %q", out)
	}
	if strings.Contains(out, "[00:00:01]") {
		t.Errorf("timestamp should be stripped: %q", out)
	}
	if !contains(tech, "build_log_strip_ansi_timestamp") {
		t.Errorf("expected build_log_strip_ansi_timestamp technique, got %v", tech)
	}
}

func TestCompressFallbackNoGrowth(t *testing.T) {
	// A tiny string should never be expanded by generic compression.
	out, saved, _ := Compress(KindGeneric, "a")
	if len(out) > 1 {
		t.Errorf("generic compression should not grow tiny input: got %q length %d", out, len(out))
	}
	if saved != 0 {
		t.Errorf("expected no savings for tiny input, got %d", saved)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
