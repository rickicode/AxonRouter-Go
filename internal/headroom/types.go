// Package headroom detects and compresses tool-output payloads so they consume
// fewer tokens when forwarded to upstream models.
package headroom

import "errors"

// Kind identifies the category of a tool-output payload.
type Kind string

const (
	KindGitDiff       Kind = "git_diff"
	KindGitLog        Kind = "git_log"
	KindGitStatus     Kind = "git_status"
	KindGrep          Kind = "grep"
	KindFindTree      Kind = "find_tree"
	KindBuildLog      Kind = "build_log"
	KindSearchResults Kind = "search_results"
	KindUnknown       Kind = "unknown"
)

// Input is the payload submitted for detection or compression.
type Input struct {
	Data []byte `json:"data"`
	// KindHint optionally suggests a payload kind. When empty the detector runs.
	KindHint Kind `json:"kind_hint,omitempty"`
}

// Output is the result of compressing a payload.
type Output struct {
	Data             []byte `json:"data"`
	Kind             Kind   `json:"kind"`
	OriginalBytes    int    `json:"original_bytes"`
	CompressedBytes  int    `json:"compressed_bytes"`
	OriginalTokens   int    `json:"original_tokens"`
	CompressedTokens int    `json:"compressed_tokens"`
}

// Detector classifies raw tool output.
type Detector interface {
	// Detect returns the most specific Kind that matches the payload.
	Detect(data []byte) Kind
}

// Compressor shrinks a payload while preserving essential information.
type Compressor interface {
	// Compress returns a smaller representation of data for the given kind.
	// The kind must be one of the defined Kind constants.
	Compress(data []byte, kind Kind) ([]byte, error)
}

// errors
var (
	ErrEmptyPayload = errors.New("headroom: empty payload")
	ErrKindUnknown  = errors.New("headroom: unknown kind")
)

// Stats reports size and token savings for a compression run.
type Stats struct {
	OriginalBytes    int
	CompressedBytes  int
	OriginalTokens   int
	CompressedTokens int
}
