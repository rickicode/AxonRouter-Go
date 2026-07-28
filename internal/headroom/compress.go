package headroom

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/klauspost/compress/zstd"
)

// DefaultCompressor is the production compressor.
type DefaultCompressor struct {
	encoder *zstd.Encoder
	decoder *zstd.Decoder
}

// NewDefaultCompressor creates a compressor with a reusable zstd encoder/decoder.
func NewDefaultCompressor() (*DefaultCompressor, error) {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		return nil, fmt.Errorf("headroom: create zstd encoder: %w", err)
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("headroom: create zstd decoder: %w", err)
	}
	return &DefaultCompressor{encoder: enc, decoder: dec}, nil
}

// ID returns the compressor identifier.
func (c *DefaultCompressor) ID() string { return "default" }

// Compress applies a kind-specific transform then zstd compresses.
func (c *DefaultCompressor) Compress(kind Kind, payload []byte) ([]byte, error) {
	if c == nil {
		return nil, errors.New("headroom: nil compressor")
	}
	transformed, method, err := transform(kind, payload)
	if err != nil {
		return nil, err
	}
	_ = method
	compressed := c.encoder.EncodeAll(transformed, nil)
	return compressed, nil
}

// Decompress reverses zstd compression (used for round-trip tests).
func (c *DefaultCompressor) Decompress(payload []byte) ([]byte, error) {
	if c == nil {
		return nil, errors.New("headroom: nil compressor")
	}
	return c.decoder.DecodeAll(payload, nil)
}

// DecompressReader returns a reader for streaming decompression.
// / DecompressReader returns a streaming reader for decompression.
func (c *DefaultCompressor) DecompressReader(r io.Reader) (io.ReadCloser, error) {
	if err := c.decoder.Reset(r); err != nil {
		return nil, err
	}
	return c.decoder.IOReadCloser(), nil
}

// transform returns a transformed payload and the method name. The method string
// is intentionally exported through Output via the calling code.
func transform(kind Kind, payload []byte) ([]byte, string, error) {
	switch kind {
	case KindGitDiff:
		return compressGitDiff(payload)
	case KindGitLog:
		return compressGitLog(payload)
	case KindGitStatus:
		return compressGitStatus(payload)
	case KindGrep:
		return compressGrep(payload)
	case KindFindTree:
		return compressFindTree(payload)
	case KindBuildLog:
		return compressBuildLog(payload)
	case KindSearchResults:
		return compressSearchResults(payload)
	case KindUnknown:
		return gzipRaw(payload), "gzip", nil
	default:
		return gzipRaw(payload), "gzip", nil
	}
}

// TransformMethod returns the method name for a kind.
func TransformMethod(kind Kind) string {
	switch kind {
	case KindGitDiff:
		return "git_diff_delta"
	case KindGitLog:
		return "git_log_tokens"
	case KindGitStatus:
		return "git_status_tokens"
	case KindGrep:
		return "grep_indexed"
	case KindFindTree:
		return "find_tree_hierarchy"
	case KindBuildLog:
		return "build_log_signature"
	case KindSearchResults:
		return "search_results_minify"
	default:
		return "gzip"
	}
}

// CompressWithMethod is a helper used by Service to fill Output.Method.
func CompressWithMethod(c Compressor, kind Kind, payload []byte) ([]byte, string, error) {
	if kind == "" || kind == KindUnknown {
		kind = KindUnknown
	}
	out, err := c.Compress(kind, payload)
	return out, TransformMethod(kind), err
}

func gzipRaw(payload []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, _ = w.Write(payload)
	_ = w.Close()
	return buf.Bytes()
}

func compressGitDiff(payload []byte) ([]byte, string, error) {
	lines := strings.Split(string(payload), "\n")
	var out strings.Builder
	inHunk := false
	for i, line := range lines {
		if i > 0 {
			out.WriteByte('\n')
		}
		switch {
		case strings.HasPrefix(line, "diff --git"):
			out.WriteString(line)
			inHunk = false
		case strings.HasPrefix(line, "index "):
			// drop full blob hashes, keep mode
			if idx := strings.Index(line, " "); idx > 0 {
				out.WriteString("index ")
				out.WriteString(line[idx+1:])
			} else {
				out.WriteString(line)
			}
		case strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ "):
			out.WriteString(line)
		case strings.HasPrefix(line, "@"):
			out.WriteString(line)
			inHunk = true
		case inHunk && strings.HasPrefix(line, "-"):
			out.WriteByte('-')
			out.WriteString(elideLongLine(line[1:]))
		case inHunk && strings.HasPrefix(line, "+"):
			out.WriteByte('+')
			out.WriteString(elideLongLine(line[1:]))
		case inHunk && (line == " " || strings.HasPrefix(line, " ")):
			// drop context lines inside hunks to shrink diff
			continue
		default:
			out.WriteString(line)
		}
	}
	return []byte(out.String()), "git_diff_delta", nil
}

func compressGitLog(payload []byte) ([]byte, string, error) {
	text := string(payload)
	re := regexp.MustCompile(`(?m)^(commit\s+[a-f0-9]{40})`)
	text = re.ReplaceAllString(text, "${1}")
	// Shorten hashes to 8 chars.
	text = regexp.MustCompile(`(?m)^(commit\s+)([a-f0-9]{8})[a-f0-9]{32}`).ReplaceAllString(text, "${1}${2}")
	return []byte(text), "git_log_tokens", nil
}

func compressGitStatus(payload []byte) ([]byte, string, error) {
	lines := strings.Split(string(payload), "\n")
	var out strings.Builder
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		// Keep path and status, remove decoration
		if strings.HasPrefix(trim, "(use") || strings.HasPrefix(trim, "no changes") {
			continue
		}
		if strings.Contains(trim, "->") {
			parts := strings.Split(trim, "->")
			if len(parts) == 2 {
				out.WriteString(strings.TrimSpace(parts[len(parts)-1]))
				out.WriteByte('\n')
				continue
			}
		}
		out.WriteString(trim)
		out.WriteByte('\n')
	}
	return []byte(out.String()), "git_status_tokens", nil
}

func compressGrep(payload []byte) ([]byte, string, error) {
	lines := strings.Split(string(payload), "\n")
	type hit struct{ Path, Line, Match string }
	var hits []hit
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 2 {
			continue
		}
		h := hit{Path: parts[0], Line: strings.TrimSpace(parts[1])}
		if len(parts) == 3 {
			h.Match = elideLongLine(parts[2])
		}
		hits = append(hits, h)
	}
	buf, err := json.Marshal(hits)
	return buf, "grep_indexed", err
}

func compressFindTree(payload []byte) ([]byte, string, error) {
	lines := strings.Split(string(payload), "\n")
	type node struct {
		Path     string `json:"p"`
		IsDir    bool   `json:"d,omitempty"`
		Children []node `json:"c,omitempty"`
	}
	var roots []node
	var stack []node
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		depth := treeDepth(line)
		name := treeName(trim)
		isDir := strings.HasSuffix(name, "/") || (!strings.Contains(name, ".") && !strings.HasPrefix(name, "."))
		n := node{Path: name, IsDir: isDir}
		if len(stack) == 0 {
			roots = append(roots, n)
			stack = []node{n}
			continue
		}
		if depth > len(stack)-1 {
			parent := &stack[len(stack)-1]
			parent.Children = append(parent.Children, n)
			stack = append(stack, n)
		} else {
			stack = stack[:depth]
			if len(stack) == 0 {
				roots = append(roots, n)
				stack = []node{n}
			} else {
				parent := &stack[len(stack)-1]
				parent.Children = append(parent.Children, n)
				stack = append(stack, n)
			}
		}
	}
	buf, err := json.Marshal(roots)
	return buf, "find_tree_hierarchy", err
}

func compressBuildLog(payload []byte) ([]byte, string, error) {
	lines := strings.Split(string(payload), "\n")
	type entry struct {
		Level   string `json:"l,omitempty"`
		Message string `json:"m"`
	}
	var entries []entry
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		lvl := ""
		lower := strings.ToLower(trim)
		switch {
		case strings.Contains(lower, "error") || strings.Contains(lower, "fail"):
			lvl = "E"
		case strings.Contains(lower, "warning") || strings.Contains(lower, "warn"):
			lvl = "W"
		case strings.Contains(lower, "success") || strings.Contains(lower, "pass") || strings.Contains(lower, "ok"):
			lvl = "S"
		case strings.HasPrefix(trim, "["):
			if end := strings.Index(trim, "]"); end > 1 {
				lvl = strings.Trim(trim[1:end], "[]")
			}
		}
		entries = append(entries, entry{Level: lvl, Message: elideLongLine(trim)})
	}
	buf, err := json.Marshal(entries)
	return buf, "build_log_signature", err
}

func compressSearchResults(payload []byte) ([]byte, string, error) {
	var raw interface{}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return gzipRaw(payload), "gzip", nil
	}
	rim, err := json.Marshal(raw)
	if err != nil {
		return nil, "search_results_minify", err
	}
	return rim, "search_results_minify", nil
}

func elideLongLine(line string) string {
	const max = 200
	if utf8.RuneCountInString(line) <= max {
		return line
	}
	runes := []rune(line)
	return string(runes[:max/2]) + "…" + string(runes[len(runes)-max/2:])
}

func treeDepth(line string) int {
	depth := 0
	for _, ch := range strings.TrimSuffix(line, treeName(strings.TrimSpace(line))) {
		if ch == ' ' || ch == '|' {
			depth++
		}
	}
	return depth / 4
}

func treeName(trim string) string {
	if idx := strings.LastIndex(trim, "--"); idx >= 0 {
		return trim[idx+2:]
	}
	if idx := strings.LastIndex(trim, "|-"); idx >= 0 {
		return trim[idx+2:]
	}
	return trim
}
