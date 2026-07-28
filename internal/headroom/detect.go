package headroom

import (
	"bytes"
	"strings"
)

// DefaultDetector is the package-level heuristic detector.
type DefaultDetector struct{}

// NewDefaultDetector creates a new heuristic detector.
func NewDefaultDetector() *DefaultDetector {
	return &DefaultDetector{}
}

// Detect runs all heuristics and returns the best matching Kind.
func (d *DefaultDetector) Detect(data []byte) Kind {
	if len(data) == 0 {
		return KindUnknown
	}

	s := string(data)

	switch {
	case looksLikeGitDiff(data):
		return KindGitDiff
	case looksLikeGitLog(s):
		return KindGitLog
	case looksLikeGitStatus(s):
		return KindGitStatus
	case looksLikeGrep(s):
		return KindGrep
	case looksLikeFindTree(s):
		return KindFindTree
	case looksLikeBuildLog(s):
		return KindBuildLog
	case looksLikeSearchResults(s):
		return KindSearchResults
	default:
		return KindUnknown
	}
}

func looksLikeGitDiff(data []byte) bool {
	return bytes.Contains(data, []byte("diff --git")) ||
		bytes.Contains(data, []byte("--- ")) && bytes.Contains(data, []byte("+++ "))
}

func looksLikeGitLog(s string) bool {
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		return false
	}
	hasCommit := strings.HasPrefix(lines[0], "commit ")
	hasAuthor := strings.Contains(s, "\nAuthor: ")
	hasDate := strings.Contains(s, "\nDate:   ")
	return hasCommit && hasAuthor && hasDate
}

func looksLikeGitStatus(s string) bool {
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		return false
	}
	first := lines[0]
	if strings.HasPrefix(first, "On branch ") ||
		strings.HasPrefix(first, "HEAD detached ") {
		return true
	}
	// Porcelain v1 format lines commonly start with status letters.
	for _, line := range lines {
		if len(line) >= 2 && line[0] == '#' && line[1] == ' ' {
			return true
		}
		if len(line) >= 2 && (line[0] == '?' && line[1] == '?') {
			return true
		}
		if len(line) >= 3 && line[2] == ' ' {
			switch line[0] {
			case 'M', 'A', 'D', 'R', 'C', 'U':
				return true
			}
			switch line[1] {
			case 'M', 'A', 'D', 'R', 'C', 'U':
				return true
			}
		}
	}
	return false
}

// grepLinePattern matches "path:line:text" or "path:line:col:text" patterns.
func looksLikeGrep(s string) bool {
	lines := strings.Split(s, "\n")
	matches := 0
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 2 {
			continue
		}
		if looksLikePath(parts[0]) {
			if isNumeric(parts[1]) {
				matches++
			}
		}
	}
	return matches >= 2
}

func looksLikeFindTree(s string) bool {
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		return false
	}
	pathLike := 0
	for _, line := range lines {
		if line == "" {
			continue
		}
		if looksLikePath(line) {
			pathLike++
		}
	}
	// At least 60% of non-empty lines should look like paths.
	nonEmpty := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty++
		}
	}
	if nonEmpty == 0 {
		return false
	}
	return float64(pathLike)/float64(nonEmpty) > 0.6
}

func looksLikeBuildLog(s string) bool {
	markers := []string{
		"[INFO]", "[WARN]", "[WARNING]", "[ERROR]", "[DEBUG]", "[TRACE]",
		"error:", "Error:", "ERROR:", "warning:", "Warning:", "WARNING:",
		"Build ", "Compiling", "SUCCESS", "FAILURE", "FAILED", "PASSED",
		"---", "===", ">>>", "<<<",
	}
	lines := strings.Split(s, "\n")
	hits := 0
	for _, line := range lines {
		for _, m := range markers {
			if strings.Contains(line, m) {
				hits++
				break
			}
		}
	}
	if len(lines) == 0 {
		return false
	}
	return float64(hits)/float64(len(lines)) > 0.15
}

func looksLikeSearchResults(s string) bool {
	if strings.Contains(s, "Results for") || strings.Contains(s, "Search results") {
		return true
	}
	lines := strings.Split(s, "\n")
	numberedHits := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "1. ") ||
			strings.HasPrefix(trimmed, "2. ") ||
			strings.HasPrefix(trimmed, "- ") && strings.Contains(line, "http") {
			numberedHits++
		}
	}
	if len(lines) == 0 {
		return false
	}
	return float64(numberedHits)/float64(len(lines)) > 0.2
}

func looksLikePath(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	return strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, "/") ||
		strings.Contains(s, "/") ||
		strings.HasSuffix(s, ".go") ||
		strings.HasSuffix(s, ".md") ||
		strings.HasSuffix(s, ".txt") ||
		strings.HasSuffix(s, ".json") ||
		strings.HasSuffix(s, ".yml") ||
		strings.HasSuffix(s, ".yaml")
}

func isNumeric(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
