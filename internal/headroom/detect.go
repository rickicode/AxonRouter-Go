package headroom

import (
	"bytes"
	"strings"
)

// Detect classifies the payload into a Kind. It uses explicit input.Kind if
// provided, falls back to Headers["X-Headroom-Hint"] / Input.Hint and finally
// heuristics on Payload itself.
func Detect(in Input) (Kind, float64) {
	if in.Kind != "" && in.Kind != KindUnknown {
		return in.Kind, 1.0
	}
	if hint := strings.TrimSpace(in.Hint); hint != "" {
		k := KindFromString(hint)
		if k != KindUnknown {
			return k, 0.95
		}
	}
	if in.Headers != nil {
		if h := strings.TrimSpace(in.Headers["X-Headroom-Hint"]); h != "" {
			k := KindFromString(h)
			if k != KindUnknown {
				return k, 0.95
			}
		}
	}
	return detectHeuristic(in.Payload)
}

// KindFromString maps a string to a Kind.
func KindFromString(s string) Kind {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "git_diff", "gitdiff", "diff":
		return KindGitDiff
	case "git_log", "gitlog", "log":
		return KindGitLog
	case "git_status", "gitstatus", "status", "git status":
		return KindGitStatus
	case "grep":
		return KindGrep
	case "find_tree", "findtree", "find", "tree":
		return KindFindTree
	case "build_log", "buildlog", "build":
		return KindBuildLog
	case "search_results", "searchresults", "search":
		return KindSearchResults
	default:
		return KindUnknown
	}
}

// detectHeuristic applies simple but reliable signatures.
func detectHeuristic(payload []byte) (Kind, float64) {
	if len(payload) == 0 {
		return KindUnknown, 0.0
	}
	text := string(payload)

	// git diff: lines starting with diff --git, ---, +++ or @@.
	if strings.Contains(text, "diff --git") ||
		strings.Contains(text, "--- a/") ||
		strings.Contains(text, "+++ b/") ||
		bytes.Contains(payload, []byte("@@ -")) {
		return KindGitDiff, 0.9
	}

	// git status: starts with common status markers.
	if strings.HasPrefix(text, "On branch ") ||
		strings.HasPrefix(text, "Your branch") ||
		strings.HasPrefix(text, "Changes to be committed") ||
		strings.HasPrefix(text, "Changes not staged") ||
		strings.Contains(text, "\tmodified:") ||
		strings.Contains(text, "\tnew file:") ||
		strings.Contains(text, "\tdeleted:") {
		return KindGitStatus, 0.9
	}

	// git log: repeated author/date/commit lines.
	if strings.Contains(text, "commit ") &&
		(strings.Contains(text, "Author:") || strings.Contains(text, "Date:")) &&
		strings.Contains(text, "\n\n") {
		return KindGitLog, 0.85
	}

	// grep: path:number:content pattern with newlines.
	if looksLikeGrep(text) {
		return KindGrep, 0.85
	}

	// find/tree: directory listing patterns.
	if looksLikeFindTree(text) {
		return KindFindTree, 0.85
	}

	// search results: JSON with hits/items/docs.
	if looksLikeSearchResults(text) {
		return KindSearchResults, 0.8
	}

	// build log: compiler/test/CI patterns.
	if looksLikeBuildLog(text) {
		return KindBuildLog, 0.8
	}

	return KindUnknown, 0.5
}

func looksLikeGrep(text string) bool {
	lines := strings.Split(text, "\n")
	hits := 0
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) >= 2 && len(parts[0]) > 0 {
			// path:number:match
			num := strings.TrimSpace(parts[1])
			if _, err := parseAtoi(num); err == nil {
				hits++
			}
		}
	}
	return hits >= 2 && hits >= len(lines)/2
}

func looksLikeFindTree(text string) bool {
	lines := strings.Split(text, "\n")
	if len(lines) < 2 {
		return false
	}
	hits := 0
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "./") || strings.HasPrefix(trim, "/") {
			hits++
			continue
		}
		if (strings.Contains(trim, "/") || strings.HasPrefix(trim, "|-") ||
			strings.HasPrefix(trim, "`--") || strings.HasPrefix(trim, "|--")) && !strings.Contains(trim, " ") {
			hits++
		}
	}
	return hits >= len(lines)/2 && hits >= 2
}

func looksLikeSearchResults(text string) bool {
	trim := strings.TrimSpace(text)
	if !strings.HasPrefix(trim, "{") && !strings.HasPrefix(trim, "[") {
		return false
	}
	lower := strings.ToLower(trim)
	markers := []string{"\"hits\"", "\"results\"", "\"items\"", "\"docs\"", "\"total\"", "\"score\""}
	count := 0
	for _, m := range markers {
		if strings.Contains(lower, m) {
			count++
		}
	}
	return count >= 2 || (strings.Contains(lower, "\"count\"") && strings.Contains(lower, "\"data\""))
}

func looksLikeBuildLog(text string) bool {
	signatures := []string{
		"error:", "warning:", "pass", "fail", "=== RUN", "--- PASS",
		"--- FAIL", "ok\t", "FAIL\t", "BUILD", "make[", "] Error ",
		"npm ERR", "go test", "cargo build", "pytest", "[INFO]",
		"[ERROR]", "[WARN]",
	}
	lower := strings.ToLower(text)
	count := 0
	for _, s := range signatures {
		if strings.Contains(lower, strings.ToLower(s)) {
			count++
		}
	}
	return count >= 3
}

func parseAtoi(s string) (int, error) {
	var n int
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, errParse
		}
		n = n*10 + int(ch-'0')
	}
	return n, nil
}

var errParse = &parseError{}

type parseError struct{}

func (parseError) Error() string { return "parse error" }
