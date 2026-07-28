package headroom

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/rickicode/AxonRouter-Go/internal/compression"
)

// DefaultCompressor is the package-level pure-Go compressor.
type DefaultCompressor struct{}

// NewDefaultCompressor creates a new pure-Go compressor.
func NewDefaultCompressor() *DefaultCompressor {
	return &DefaultCompressor{}
}

// Compress selects a strategy based on kind or returns the original data when
// the kind has no dedicated compressor.
func (c *DefaultCompressor) Compress(data []byte, kind Kind) ([]byte, error) {
	if len(data) == 0 {
		return nil, ErrEmptyPayload
	}

	s := string(data)
	var out string
	switch kind {
	case KindGitDiff:
		out = compressGitDiff(s)
	case KindGitLog:
		out = compressGitLog(s)
	case KindGitStatus:
		out = compressGitStatus(s)
	case KindGrep:
		out = compressGrep(s)
	case KindFindTree:
		out = compressFindTree(s)
	case KindBuildLog:
		out = compressBuildLog(s)
	case KindSearchResults:
		out = compressSearchResults(s)
	case KindUnknown:
		out = collapseWhitespace(s)
	default:
		return nil, fmt.Errorf("%w: %q", ErrKindUnknown, kind)
	}

	return []byte(out), nil
}

func compressGitDiff(s string) string {
	lines := strings.Split(s, "\n")
	var b strings.Builder
	var inHunk bool
	var kept int
	const maxContextLines = 3
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "diff --git") {
			b.WriteString(trimmed)
			b.WriteByte('\n')
			inHunk = false
			kept = 0
			continue
		}
		if strings.HasPrefix(line, "@@") {
			b.WriteString(line)
			b.WriteByte('\n')
			inHunk = true
			kept = 0
			continue
		}
		if !inHunk {
			if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") ||
				strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "+") ||
				strings.HasPrefix(line, "-") {
				b.WriteString(line)
				b.WriteByte('\n')
			}
			continue
		}
		// Inside hunk: keep +/- lines and a limited amount of context.
		if len(line) > 0 && (line[0] == '+' || line[0] == '-') {
			b.WriteString(line)
			b.WriteByte('\n')
			kept = 0
		} else if kept < maxContextLines {
			b.WriteString(line)
			b.WriteByte('\n')
			kept++
		}
	}
	return collapseWhitespace(b.String())
}

func compressGitLog(s string) string {
	lines := strings.Split(s, "\n")
	var b strings.Builder
	var inMessage bool
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "commit ") {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString("c " + strings.TrimPrefix(trimmed, "commit "))
			b.WriteByte('\n')
			inMessage = false
			continue
		}
		if strings.HasPrefix(line, "Author: ") {
			parts := strings.SplitN(trimmed, " ", 2)
			if len(parts) == 2 {
				b.WriteString("a " + parts[1])
				b.WriteByte('\n')
			}
			continue
		}
		if strings.HasPrefix(line, "Date:   ") {
			b.WriteString("d " + strings.TrimPrefix(trimmed, "Date:   "))
			b.WriteByte('\n')
			continue
		}
		if trimmed == "" {
			inMessage = true
			continue
		}
		if inMessage {
			b.WriteString("m " + trimmed)
			b.WriteByte('\n')
		}
	}
	return collapseWhitespace(b.String())
}

func compressGitStatus(s string) string {
	lines := strings.Split(s, "\n")
	var b strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(line, "On branch ") {
			b.WriteString("b " + strings.TrimPrefix(trimmed, "On branch "))
			b.WriteByte('\n')
			continue
		}
		if strings.HasPrefix(line, "Your branch") {
			continue
		}
		if strings.HasPrefix(line, "nothing to commit") {
			b.WriteString(trimmed)
			b.WriteByte('\n')
			continue
		}
		// Porcelain / short status lines.
		if len(line) >= 3 {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return collapseWhitespace(b.String())
}

var grepLinePattern = regexp.MustCompile(`^(.+?):(\d+)(?::(\d+))?:(.*)$`)

func compressGrep(s string) string {
	lines := strings.Split(s, "\n")
	groups := make(map[string][]string)
	for _, line := range lines {
		if line == "" {
			continue
		}
		m := grepLinePattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		path := m[1]
		ln := m[2]
		col := m[3]
		text := strings.TrimSpace(m[4])
		if text == "" {
			continue
		}
		ref := ln
		if col != "" {
			ref = ln + ":" + col
		}
		groups[path] = append(groups[path], ref+":"+text)
	}

	var b strings.Builder
	first := true
	for path, hits := range groups {
		if !first {
			b.WriteByte('\n')
		}
		first = false
		b.WriteString(path)
		b.WriteByte('\n')
		for _, h := range mergeHits(hits) {
			b.WriteString("  " + h)
			b.WriteByte('\n')
		}
	}
	return collapseWhitespace(b.String())
}

func mergeHits(hits []string) []string {
	if len(hits) == 0 {
		return nil
	}
	dedup := make([]string, 0, len(hits))
	seen := make(map[string]struct{}, len(hits))
	for _, h := range hits {
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		dedup = append(dedup, h)
	}
	return dedup
}

func compressFindTree(s string) string {
	lines := strings.Split(s, "\n")
	seen := make(map[string]struct{})
	var b strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Try to strip leading `find` metadata such as permissions and sizes.
		fields := strings.Fields(trimmed)
		path := trimmed
		if len(fields) >= 9 {
			path = strings.Join(fields[8:], " ")
		} else if len(fields) == 1 {
			path = fields[0]
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		b.WriteString(path)
		b.WriteByte('\n')
	}
	return collapseWhitespace(b.String())
}

func compressBuildLog(s string) string {
	lines := strings.Split(s, "\n")
	var b strings.Builder
	var last string
	drop := func(line string) bool {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			return true
		}
		// Drop progress noise.
		if strings.Contains(line, "%") && strings.Contains(line, "[") {
			return true
		}
		return false
	}
	for _, line := range lines {
		if drop(line) {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == last {
			continue
		}
		last = trimmed
		b.WriteString(trimmed)
		b.WriteByte('\n')
	}
	return collapseWhitespace(b.String())
}

func compressSearchResults(s string) string {
	lines := strings.Split(s, "\n")
	var b strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Drop or truncate URLs.
		if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
			continue
		}
		if len(trimmed) > 120 {
			trimmed = trimmed[:117] + "..."
		}
		b.WriteString(trimmed)
		b.WriteByte('\n')
	}
	return collapseWhitespace(b.String())
}

func collapseWhitespace(s string) string {
	var b strings.Builder
	var lastSpace bool
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !lastSpace {
				b.WriteByte('\n')
				lastSpace = true
			}
			continue
		}
		b.WriteRune(r)
		lastSpace = false
	}
	return strings.TrimSpace(b.String())
}

// TokenEstimate returns a rough token count for the supplied bytes.
func TokenEstimate(data []byte) int {
	return compression.EstimateTokens(string(data))
}

func roundFloat(v float64) float64 {
	return math.Round(v*100) / 100
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
