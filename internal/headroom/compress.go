package headroom

import (
	"regexp"
	"strings"
)

// Compress applies kind-specific text reductions. It always returns the
// compressed text plus statistics; the returned int is bytes saved.
func Compress(kind PayloadKind, text string) (string, int, []string) {
	originalSize := len(text)
	techniques := []string{}
	out := text

	switch kind {
	case KindGitDiff:
		out, techniques = compressGitDiff(out)
	case KindGitLog:
		out, techniques = compressGitLog(out)
	case KindGitStatus:
		out, techniques = compressGitStatus(out)
	case KindGrep:
		out, techniques = compressGrep(out)
	case KindFindTree:
		out, techniques = compressFindTree(out)
	case KindBuildLog:
		out, techniques = compressBuildLog(out)
	case KindSearch:
		out, techniques = compressSearch(out)
	case KindToolResult:
		out = collapseEmptyLines(out)
		techniques = append(techniques, "collapse_empty_lines")
	default:
		out = collapseEmptyLines(out)
		out = dedupeAdjacent(out)
		techniques = append(techniques, "generic_collapse", "dedupe_adjacent")
	}

	saved := originalSize - len(out)
	if saved < 0 {
		saved = 0
		out = text
	}
	return out, saved, techniques
}

func compressGitDiff(text string) (string, []string) {
	lines := strings.Split(text, "\n")
	var out []string
	inContext := false
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \r")
		if strings.HasPrefix(trimmed, "@@") {
			inContext = true
			if idx := strings.Index(trimmed, " @@"); idx != -1 {
				header := strings.TrimSpace(trimmed[:idx+3])
				out = append(out, header)
				continue
			}
		}
		if inContext && strings.HasPrefix(trimmed, " ") && len(trimmed) > 120 {
			trimmed = trimmed[:120] + " …"
		}
		if trimmed != "" || (len(out) > 0 && out[len(out)-1] != "") {
			out = append(out, trimmed)
		}
	}
	result := collapseEmptyLines(strings.Join(out, "\n"))
	result = strings.TrimSpace(result)
	return result, []string{"git_diff_context_trim"}
}

func compressGitLog(text string) (string, []string) {
	lines := strings.Split(text, "\n")
	var out []string
	skip := false
	for _, line := range lines {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "commit ") && len(out) > 0 {
			if out[len(out)-1] != "" {
				out = append(out, "")
			}
			skip = false
		}
		if strings.HasPrefix(s, "Author:") || strings.HasPrefix(s, "Date:") {
			out = append(out, s)
			continue
		}
		if strings.HasPrefix(s, "diff --git") {
			skip = true
			continue
		}
		if skip {
			continue
		}
		out = append(out, line)
	}
	return collapseEmptyLines(strings.Join(out, "\n")), []string{"git_log_strip_patches"}
}

func compressGitStatus(text string) (string, []string) {
	lines := strings.Split(text, "\n")
	var out []string
	for _, line := range lines {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		out = append(out, s)
	}
	return strings.Join(out, "\n"), []string{"git_status_clean"}
}

func compressGrep(text string) (string, []string) {
	lines := strings.Split(text, "\n")
	var out []string
	prevPrefix := ""
	for _, line := range lines {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		parts := strings.SplitN(s, ":", 3)
		if len(parts) >= 2 {
			prefix := parts[0] + ":" + parts[1]
			if prefix == prevPrefix {
				continue
			}
			prevPrefix = prefix
		}
		out = append(out, s)
	}
	return strings.Join(out, "\n"), []string{"grep_dedupe"}
}

func compressFindTree(text string) (string, []string) {
	lines := strings.Split(text, "\n")
	var out []string
	for _, line := range lines {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		out = append(out, s)
	}
	return strings.Join(out, "\n"), []string{"find_tree_clean"}
}

func compressBuildLog(text string) (string, []string) {
	lines := strings.Split(text, "\n")
	var out []string
	lastWasBlank := false
	for _, line := range lines {
		s := strings.TrimSpace(line)
		if s == "" {
			if !lastWasBlank {
				out = append(out, "")
				lastWasBlank = true
			}
			continue
		}
		lastWasBlank = false
		clean := stripANSI(s)
		clean = stripLeadingTimestamp(clean)
		if clean != "" {
			out = append(out, clean)
		}
	}
	return strings.Join(out, "\n"), []string{"build_log_strip_ansi_timestamp"}
}

func compressSearch(text string) (string, []string) {
	lines := strings.Split(text, "\n")
	var out []string
	for _, line := range lines {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return strings.Join(out, "\n"), []string{"search_clean"}
}

func collapseEmptyLines(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	lastBlank := false
	for _, line := range lines {
		blank := strings.TrimSpace(line) == ""
		if blank {
			if !lastBlank {
				out = append(out, "")
				lastBlank = true
			}
			continue
		}
		lastBlank = false
		out = append(out, strings.TrimRight(line, " \r"))
	}
	return strings.Join(out, "\n")
}

func dedupeAdjacent(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	var prev string
	for _, line := range lines {
		if line == prev {
			continue
		}
		out = append(out, line)
		prev = line
	}
	return strings.Join(out, "\n")
}

func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

var timestampRes = []*regexp.Regexp{
	regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?\s*`),
	regexp.MustCompile(`^\[\d{2}:\d{2}:\d{2}\]\s*`),
	regexp.MustCompile(`^\d{2}:\d{2}:\d{2}\s+`),
}

func stripLeadingTimestamp(s string) string {
	s = strings.TrimSpace(s)
	for _, re := range timestampRes {
		s = re.ReplaceAllString(s, "")
	}
	return strings.TrimSpace(s)
}
