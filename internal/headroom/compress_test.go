package headroom

import "testing"

func TestCompressSmallDiffSaves(t *testing.T) {
	diff := "diff --git a/foo.go b/foo.go\n--- a/foo.go\n+++ b/foo.go\n@@ -1,5 +1,5 @@\n package main\n unchanged line one\n unchanged line two\n-hello\n+world\n"
	out, saved, tech := Compress(KindGitDiff, diff)
	t.Logf("saved=%d tech=%v out=%q", saved, tech, out)
	if saved <= 0 {
		t.Fatalf("expected positive savings, got %d", saved)
	}
}
