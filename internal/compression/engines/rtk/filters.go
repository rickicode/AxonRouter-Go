package rtk

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	rawCap                  = 10 * 1024 * 1024
	minCompressSize         = 100
	detectWindow            = 1024
	gitDiffHunkMaxLines     = 100
	gitDiffMaxLines         = 500
	gitLogMaxLines          = 200
	dedupLineMax            = 2000
	grepPerFileMax          = 10
	findPerDirMax           = 10
	findTotalDirMax         = 20
	statusMaxFiles          = 10
	statusMaxUntracked      = 10
	lsExtSummaryTop         = 5
	treeMaxLines            = 200
	searchListPerDirMax     = 10
	searchListTotalDirMax   = 20
	smartTruncateHead       = 120
	smartTruncateTail       = 60
	smartTruncateMinLines   = 250
	readNumberedMinHitRatio = 0.7
)

var lsNoiseDirs = []string{
	"node_modules", ".git", "target", "__pycache__",
	".next", "dist", "build", ".cache", ".turbo",
	".vercel", ".pytest_cache", ".mypy_cache", ".tox",
	".venv", "venv", "env", "coverage", ".nyc_output",
	".DS_Store", "Thumbs.db", ".idea", ".vscode", ".vs",
	"*.egg-info", ".eggs",
}

var (
	reGitDiff       = regexp.MustCompile(`(?m)^diff --git `)
	reGitDiffHunk   = regexp.MustCompile(`(?m)^@@ `)
	reGitStatus     = regexp.MustCompile(`(?m)^(On branch |nothing to commit|Changes (not |to be )|Untracked files:)`)
	reGitLog        = regexp.MustCompile(`(?m)^[\*|/\\ ]*commit [0-9a-f]{7,40}$`)
	rePorcelain     = regexp.MustCompile(`^[ MADRCU?!][ MADRCU?!] \S`)
	reTreeGlyph     = regexp.MustCompile(`[├└]──|│ `)
	reLsRow         = regexp.MustCompile(`(?m)^[-dlbcps][rwx-]{9}`)
	reLsTotal       = regexp.MustCompile(`(?m)^total \d+$`)
	reLsDate        = regexp.MustCompile(`\s+(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+\d{1,2}\s+(\d{4}|\d{2}:\d{2})\s+`)
	reReadNumbered  = regexp.MustCompile(`^\s*\d+\|`)
	reSearchListHdr = regexp.MustCompile(`^Result of search in '[^']*' \(total (\d+) files?\):`)
	reGitLogOneline = regexp.MustCompile(`(?m)^[\*|/\\ ]*([0-9a-f]{7,40}\s+.+)$`)
	reBuildOutput   = regexp.MustCompile(`(?m)^(npm (warn|error|notice)|warning[: ]|error[: ]|> )`)
)

func autoDetectFilter(text string) (string, func(string) string) {
	head := text
	if len(head) > detectWindow {
		head = head[:detectWindow]
	}

	if reBuildOutput.MatchString(head) {
		return "build-output", filterBuildOutput
	}
	if reGitLog.MatchString(head) {
		return "git-log", filterGitLog
	}
	if reGitDiff.MatchString(head) || reGitDiffHunk.MatchString(head) {
		return "git-diff", filterGitDiff
	}
	if reGitStatus.MatchString(head) {
		return "git-status", filterGitStatus
	}
	if isMostlyPorcelain(head) {
		return "git-status", filterGitStatus
	}

	lines := strings.Split(head, "\n")
	nonEmpty := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty = append(nonEmpty, l)
		}
	}

	first5 := nonEmpty
	if len(first5) > 5 {
		first5 = first5[:5]
	}
	for _, l := range first5 {
		if isGrepLine(l) {
			return "grep", filterGrep
		}
	}

	if len(nonEmpty) >= 3 {
		allPathLike := true
		for _, l := range nonEmpty {
			if !isPathLike(l) {
				allPathLike = false
				break
			}
		}
		if allPathLike {
			return "find", filterFind
		}
	}

	if reTreeGlyph.MatchString(head) {
		return "tree", filterTree
	}

	if reLsTotal.MatchString(head) || countMatches(head, reLsRow) >= 3 {
		return "ls", filterLs
	}

	if reSearchListHdr.MatchString(head) {
		return "search-list", filterSearchList
	}

	if len(lines) >= smartTruncateMinLines && isLineNumbered(lines) {
		return "read-numbered", filterReadNumbered
	}

	if len(nonEmpty) >= 5 {
		return "dedup-log", filterDedupLog
	}

	if len(lines) >= smartTruncateMinLines {
		return "smart-truncate", filterSmartTruncate
	}

	return "", nil
}

func safeApply(fn func(string) string, text string) string {
	if fn == nil {
		return text
	}
	defer func() { _ = recover() }()
	out := fn(text)
	if out == "" || len(out) >= len(text) {
		return text
	}
	return out
}

func countMatches(text string, re *regexp.Regexp) int {
	return len(re.FindAllString(text, -1))
}

func isGrepLine(line string) bool {
	first := strings.Index(line, ":")
	if first == -1 {
		return false
	}
	second := strings.Index(line[first+1:], ":")
	if second == -1 {
		return false
	}
	lineno := line[first+1 : first+1+second]
	_, err := strconv.Atoi(lineno)
	return err == nil
}

func isPathLike(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	if regexp.MustCompile(`^[A-Za-z]:[\\/]`).MatchString(t) {
		return true
	}
	if strings.Contains(t, ":") {
		return false
	}
	return strings.HasPrefix(t, ".") || strings.HasPrefix(t, "/") || strings.Contains(t, "/") || strings.Contains(t, "\\")
}

func isMostlyPorcelain(head string) bool {
	lines := strings.Split(head, "\n")
	var total, hits int
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		total++
		if rePorcelain.MatchString(l) {
			hits++
		}
	}
	return total >= 3 && float64(hits)/float64(total) >= 0.6
}

func isLineNumbered(lines []string) bool {
	var hits, nonEmpty int
	limit := 100
	if len(lines) < limit {
		limit = len(lines)
	}
	for i := 0; i < limit; i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" {
			continue
		}
		nonEmpty++
		if reReadNumbered.MatchString(l) {
			hits++
		}
	}
	if nonEmpty < 5 {
		return false
	}
	return float64(hits)/float64(nonEmpty) >= readNumberedMinHitRatio
}

func filterGrep(input string) string {
	lines := strings.Split(input, "\n")
	type entry struct {
		lines []string
		count int
	}
	groups := make(map[string]*entry)
	var order []string
	for _, l := range lines {
		path, rest, ok := splitGrepLine(l)
		if !ok {
			continue
		}
		e, ok := groups[path]
		if !ok {
			e = &entry{}
			groups[path] = e
			order = append(order, path)
		}
		e.count++
		if len(e.lines) < grepPerFileMax {
			e.lines = append(e.lines, rest)
		}
	}
	if len(order) == 0 {
		return input
	}
	var out strings.Builder
	for i, path := range order {
		if i >= findTotalDirMax {
			break
		}
		e := groups[path]
		out.WriteString(path)
		out.WriteString("\n")
		for _, rest := range e.lines {
			out.WriteString("  ")
			out.WriteString(rest)
			out.WriteString("\n")
		}
		if e.count > len(e.lines) {
			out.WriteString(fmt.Sprintf("  ... (%d more matches)\n", e.count-len(e.lines)))
		}
	}
	return strings.TrimSpace(out.String())
}
func splitGrepLine(line string) (string, string, bool) {
	first := strings.Index(line, ":")
	if first == -1 {
		return "", "", false
	}
	second := strings.Index(line[first+1:], ":")
	if second == -1 {
		return "", "", false
	}
	path := line[:first]
	lineno := line[first+1 : first+1+second]
	if _, err := strconv.Atoi(lineno); err != nil {
		return "", "", false
	}
	rest := line[first+1:]
	return path, rest, true
}

func filterGitDiff(input string) string {
	lines := strings.Split(input, "\n")
	var out []string
	total := 0
	diffRun := 0
	skippingHunk := false

	for i := 0; i < len(lines) && total < gitDiffMaxLines; i++ {
		l := lines[i]

		if strings.HasPrefix(l, "diff --git") {
			skippingHunk = false
			diffRun = 0
			out = append(out, l)
			total++
			continue
		}

		if strings.HasPrefix(l, "@@") {
			skippingHunk = false
			diffRun = 0
			out = append(out, l)
			total++
			hunkLines := 0
			for i+1 < len(lines) && total < gitDiffMaxLines {
				next := lines[i+1]
				if isDiffBoundary(next) {
					break
				}
				if hunkLines >= gitDiffHunkMaxLines {
					out = append(out, "  ... (hunk truncated)")
					total++
					for i+1 < len(lines) && !isDiffBoundary(lines[i+1]) {
						i++
					}
					break
				}
				out = append(out, next)
				total++
				hunkLines++
				i++
			}
			continue
		}

		if strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-") {
			if skippingHunk {
				continue
			}
			diffRun++
			if diffRun > gitDiffHunkMaxLines {
				skippingHunk = true
				out = append(out, "  ... (diff body truncated)")
				total++
				continue
			}
			out = append(out, l)
			total++
			continue
		}

		diffRun = 0
		skippingHunk = false
		out = append(out, l)
		total++
	}

	if total >= gitDiffMaxLines && len(out) > 0 {
		out = append(out, "... (diff truncated)")
	}
	return strings.Join(out, "\n")
}

func isDiffBoundary(line string) bool {
	return strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "@@")
}
func filterGitStatus(input string) string {
	lines := strings.Split(input, "\n")
	var out []string
	section := ""
	var sectionFiles []string
	flush := func() {
		if section == "" {
			return
		}
		out = append(out, section)
		limit := statusMaxFiles
		if strings.Contains(section, "Untracked") {
			limit = statusMaxUntracked
		}
		for i, f := range sectionFiles {
			if i >= limit {
				out = append(out, fmt.Sprintf("  ... (%d more %s)", len(sectionFiles)-limit, fileWord(section)))
				break
			}
			out = append(out, f)
		}
		section = ""
		sectionFiles = nil
	}

	for _, l := range lines {
		trim := strings.TrimSpace(l)
		if strings.HasPrefix(l, "On branch ") {
			flush()
			out = append(out, l)
			continue
		}
		if strings.HasPrefix(l, "Your branch") {
			out = append(out, l)
			continue
		}
		if strings.Contains(l, "Changes to be committed") || strings.Contains(l, "Changes not staged") || strings.Contains(l, "Untracked files") {
			flush()
			section = l
			continue
		}
		if section != "" && (trim != "") {
			if strings.HasPrefix(l, "\t") || strings.HasPrefix(l, "  ") || rePorcelain.MatchString(l) {
				sectionFiles = append(sectionFiles, l)
			}
		}
	}
	flush()
	return strings.Join(out, "\n")
}

func fileWord(section string) string {
	if strings.Contains(section, "Untracked") {
		return "untracked"
	}
	return "files"
}
func filterGitLog(input string) string {
	lines := strings.Split(input, "\n")
	var out []string
	for _, l := range lines {
		m := reGitLogOneline.FindStringSubmatch(l)
		if len(m) == 2 {
			out = append(out, m[1])
		} else if strings.TrimSpace(l) != "" && !strings.Contains(l, "commit") {
			out = append(out, strings.TrimSpace(l))
		}
		if len(out) >= gitLogMaxLines {
			out = append(out, fmt.Sprintf("... (%d more commits)", len(lines)-len(out)))
			break
		}
	}
	return strings.Join(out, "\n")
}

func filterFind(input string) string {
	lines := strings.Split(input, "\n")
	type entry struct {
		files []string
	}
	groups := make(map[string]*entry)
	var order []string
	for _, l := range lines {
		trim := strings.TrimSpace(l)
		if trim == "" {
			continue
		}
		dir := pathDir(trim)
		if dir == "" {
			dir = "."
		}
		e, ok := groups[dir]
		if !ok {
			e = &entry{}
			groups[dir] = e
			order = append(order, dir)
		}
		if len(e.files) < findPerDirMax {
			e.files = append(e.files, trim)
		}
	}
	if len(order) == 0 {
		return input
	}
	var out strings.Builder
	for i, dir := range order {
		if i >= findTotalDirMax {
			out.WriteString(fmt.Sprintf("... (%d more dirs)\n", len(order)-i))
			break
		}
		e := groups[dir]
		out.WriteString(dir)
		out.WriteString("/\n")
		for _, f := range e.files {
			out.WriteString("  ")
			out.WriteString(pathBase(f))
			out.WriteString("\n")
		}
		if len(groups[dir].files) >= findPerDirMax {
			out.WriteString("  ... (more)\n")
		}
	}
	return strings.TrimSpace(out.String())
}

func pathDir(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "./")
	i := strings.LastIndex(p, "/")
	if i == -1 {
		i = strings.LastIndex(p, "\\")
	}
	if i == -1 {
		return ""
	}
	return p[:i]
}

func pathBase(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "./")
	i := strings.LastIndex(p, "/")
	if i == -1 {
		i = strings.LastIndex(p, "\\")
	}
	if i == -1 {
		return p
	}
	return p[i+1:]
}

func filterLs(input string) string {
	lines := strings.Split(input, "\n")
	var dirs, files []string
	for _, l := range lines {
		l = strings.TrimRight(l, "\r")
		if reLsTotal.MatchString(l) {
			continue
		}
		if !reLsRow.MatchString(l) {
			continue
		}
		l = reLsDate.ReplaceAllString(l, " ")
		l = strings.TrimSpace(l)
		parts := strings.Fields(l)
		if len(parts) < 2 {
			continue
		}
		name := parts[len(parts)-1]
		mode := parts[0]
		if strings.HasPrefix(mode, "d") {
			dirs = append(dirs, name+"/")
		} else {
			files = append(files, name)
		}
	}
	if len(dirs) == 0 && len(files) == 0 {
		return input
	}
	var out strings.Builder
	if len(dirs) > 0 {
		out.WriteString(fmt.Sprintf("dirs: %s\n", strings.Join(dirs, ", ")))
	}
	if len(files) > 0 {
		out.WriteString(fmt.Sprintf("files: %s\n", strings.Join(files, ", ")))
	}
	return strings.TrimSpace(out.String())
}

func filterTree(input string) string {
	lines := strings.Split(strings.TrimSpace(input), "\n")
	if len(lines) == 0 {
		return input
	}
	last := lines[len(lines)-1]
	if strings.Contains(last, "director") || strings.Contains(last, "file") {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > treeMaxLines {
		lines = lines[:treeMaxLines]
		lines = append(lines, "... (tree truncated)")
	}
	return strings.Join(lines, "\n")
}

func filterDedupLog(input string) string {
	lines := strings.Split(input, "\n")
	var out []string
	var current string
	count := 0
	flush := func() {
		if count == 0 {
			return
		}
		if count == 1 {
			out = append(out, current)
		} else {
			out = append(out, fmt.Sprintf("%s (x%d)", current, count))
		}
	}
	for _, l := range lines {
		trim := strings.TrimSpace(l)
		if trim == "" {
			continue
		}
		if trim == current {
			count++
			continue
		}
		flush()
		current = trim
		count = 1
	}
	flush()
	if len(out) > dedupLineMax {
		out = append(out[:dedupLineMax], fmt.Sprintf("... (%d more lines)", len(out)-dedupLineMax))
	}
	return strings.Join(out, "\n")
}
func filterSmartTruncate(input string) string {
	lines := strings.Split(input, "\n")
	if len(lines) < smartTruncateMinLines {
		return input
	}
	out := make([]string, 0, smartTruncateHead+smartTruncateTail+2)
	out = append(out, lines[:smartTruncateHead]...)
	out = append(out, fmt.Sprintf("... (%d lines omitted) ...", len(lines)-smartTruncateHead-smartTruncateTail))
	out = append(out, lines[len(lines)-smartTruncateTail:]...)
	return strings.Join(out, "\n")
}

func filterReadNumbered(input string) string {
	lines := strings.Split(input, "\n")
	if len(lines) < smartTruncateMinLines {
		return input
	}
	out := make([]string, 0, smartTruncateHead+smartTruncateTail+2)
	out = append(out, lines[:smartTruncateHead]...)
	out = append(out, "... (lines omitted) ...")
	out = append(out, lines[len(lines)-smartTruncateTail:]...)
	return strings.Join(out, "\n")
}

func filterSearchList(input string) string {
	lines := strings.Split(input, "\n")
	if len(lines) == 0 {
		return input
	}
	m := reSearchListHdr.FindStringSubmatch(lines[0])
	total := 0
	if len(m) == 2 {
		total, _ = strconv.Atoi(m[1])
	}
	var files []string
	for _, l := range lines[1:] {
		l = strings.TrimSpace(l)
		l = strings.TrimPrefix(l, "- ")
		if l != "" && len(files) < searchListPerDirMax {
			files = append(files, l)
		}
	}
	var out strings.Builder
	out.WriteString(lines[0])
	out.WriteString("\n")
	for _, f := range files {
		out.WriteString("- ")
		out.WriteString(f)
		out.WriteString("\n")
	}
	if total > len(files) {
		out.WriteString(fmt.Sprintf("... (%d more files)\n", total-len(files)))
	}
	return strings.TrimSpace(out.String())
}

func filterBuildOutput(input string) string {
	lines := strings.Split(input, "\n")
	type warningEntry struct {
		count int
		first string
	}
	warnings := make(map[string]*warningEntry)
	var order []string
	var errors []string
	for _, l := range lines {
		trim := strings.TrimSpace(l)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "error") || strings.HasPrefix(trim, "npm error") {
			errors = append(errors, trim)
			continue
		}
		key := normalizeWarning(trim)
		e, ok := warnings[key]
		if !ok {
			e = &warningEntry{first: trim}
			warnings[key] = e
			order = append(order, key)
		}
		e.count++
	}
	if len(warnings) == 0 && len(errors) == 0 {
		return input
	}
	var out strings.Builder
	for _, key := range order {
		e := warnings[key]
		if e.count == 1 {
			out.WriteString(e.first)
		} else {
			out.WriteString(fmt.Sprintf("%s (x%d)", e.first, e.count))
		}
		out.WriteString("\n")
	}
	for _, e := range errors {
		out.WriteString(e)
		out.WriteString("\n")
	}
	return strings.TrimSpace(out.String())
}

func normalizeWarning(line string) string {
	line = strings.TrimPrefix(line, "npm warn")
	line = strings.TrimPrefix(line, "warning")
	line = strings.TrimSpace(line)
	return strings.ToLower(line)
}
