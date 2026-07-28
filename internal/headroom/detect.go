package headroom

import (
	"regexp"
	"strings"
)

var (
	diffHunkRE     = regexp.MustCompile(`(?m)^(?:diff --git|@@ -\d+)`)
	gitLogRE       = regexp.MustCompile(`(?m)^commit [0-9a-f]{7,40}`)
	gitStatusRE    = regexp.MustCompile(`(?m)^\s*(?:M|A|D|R|C|U|\?\?)\s+`)
	grepPrefixRE   = regexp.MustCompile(`(?m)^[^:]+:\d+[:\-]`)
	findTreeRE     = regexp.MustCompile(`(?m)^(?:\./)?(?:[^/\n]+/)*[^/\n]+$`)
	buildLogRE     = regexp.MustCompile(`(?m)(?:\[\d{2,}m|error|warning|FAIL|PASS|ok\s+|---\s+)`)
	searchResultRE = regexp.MustCompile(`(?m)(?:^\s*\d+\.\s|^\s*[-•]\s|^\s*#\s+Result|^\s*Path:\s)`)
)

// DetectPayloadKind heuristically classifies tool outputs.
func DetectPayloadKind(text string) PayloadKind {
	if text == "" {
		return KindGeneric
	}
	lines := strings.Split(text, "\n")
	nonEmpty := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty++
		}
	}

	if diffHunkRE.MatchString(text) && strings.Contains(text, "--- ") {
		return KindGitDiff
	}
	if gitLogRE.MatchString(text) && strings.Count(text, "\ncommit ") >= 1 {
		return KindGitLog
	}
	if gitStatusRE.MatchString(text) && nonEmpty <= 200 && !strings.Contains(text, "diff --git") {
		return KindGitStatus
	}
	if grepPrefixRE.MatchString(text) {
		return KindGrep
	}
	if buildLogRE.MatchString(text) && (strings.Contains(text, "FAIL") || strings.Contains(text, "error") || strings.Contains(text, "PASS") || strings.Contains(text, "ok ")) {
		return KindBuildLog
	}
	if searchResultRE.MatchString(text) && nonEmpty > 2 {
		return KindSearch
	}
	if findTreeRE.MatchString(text) && nonEmpty > 3 {
		matching := 0
		for _, line := range lines {
			s := strings.TrimSpace(line)
			if s != "" && !strings.ContainsAny(s, "|<>*?\":") {
				matching++
			}
		}
		if matching >= nonEmpty*3/4 {
			return KindFindTree
		}
	}

	return KindGeneric
}
