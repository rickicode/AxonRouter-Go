package headroom

import (
	"testing"
)

func TestDetect_ExplicitKind(t *testing.T) {
	in := Input{Kind: KindGitDiff, Payload: []byte("anything")}
	kind, conf := Detect(in)
	if kind != KindGitDiff || conf != 1.0 {
		t.Fatalf("expected git_diff with confidence 1.0, got %s %f", kind, conf)
	}
}

func TestDetect_Hint(t *testing.T) {
	tests := []struct {
		hint string
		want Kind
	}{
		{"git_log", KindGitLog},
		{"git status", KindGitStatus},
		{"grep", KindGrep},
		{"find_tree", KindFindTree},
		{"build_log", KindBuildLog},
		{"search_results", KindSearchResults},
		{"unknown", KindUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.hint, func(t *testing.T) {
			in := Input{Hint: tt.hint, Payload: []byte("x")}
			kind, _ := Detect(in)
			if kind != tt.want {
				t.Fatalf("hint %q: expected %s, got %s", tt.hint, tt.want, kind)
			}
		})
	}
}

func TestDetect_Heuristics(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    Kind
	}{
		{
			name:    "git_diff",
			payload: "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n@@ -1,3 +1,3 @@\n x\n-y\n+z\n",
			want: KindGitDiff,
		},
		{
			name:    "git_status",
			payload: "On branch main\nYour branch is up to date.\n\nChanges not staged for commit:\n\tmodified:   foo.go\n",
			want: KindGitStatus,
		},
		{
			name: "git_log",
			payload: `commit abcdef1234567890abcdef1234567890abcdef12
Author: Test User <test@example.com>
Date:   Mon Jan 1 00:00:00 2024 +0000

    first commit

commit abcdef1234567890abcdef1234567890abcdef13
Author: Test User <test@example.com>
Date:   Mon Jan 2 00:00:00 2024 +0000

    second commit
`,
			want: KindGitLog,
		},
		{
			name:    "grep",
			payload: "internal/foo.go:42:func Foo() {}\ninternal/bar.go:7:type Bar struct{}\n",
			want:    KindGrep,
		},
		{
			name:    "find_tree",
			payload: "./cmd\n./cmd/server\n./cmd/server/main.go\n./internal\n./internal/headroom\n",
			want:    KindFindTree,
		},
		{
			name:    "build_log",
			payload: "=== RUN TestSomething\n--- PASS: TestSomething (0.00s)\nok\texample.com/foo\tbaz\n",
			want:    KindBuildLog,
		},
		{
			name:    "search_results",
			payload: `{"total": 2, "hits": [{"_score": 1.0, "_source": {"x": 1}}, {"_score": 0.5, "_source": {"x": 2}}]}`,
			want:    KindSearchResults,
		},
		{
			name:    "unknown",
			payload: "hello world",
			want:    KindUnknown,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := Input{Payload: []byte(tt.payload)}
			kind, _ := Detect(in)
			if kind != tt.want {
				t.Fatalf("expected %s, got %s", tt.want, kind)
			}
		})
	}
}

func TestKindFromString(t *testing.T) {
	if KindFromString("DIFF") != KindGitDiff {
		t.Fatal("expected case-insensitive match for DIFF")
	}
	if KindFromString("not-a-kind") != KindUnknown {
		t.Fatal("expected unknown")
	}
}
