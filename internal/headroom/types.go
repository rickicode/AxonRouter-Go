package headroom

import (
	"context"
)

// Kind represents the classification of a payload.
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

// Input is the payload to be classified and compressed.
type Input struct {
	Payload []byte            `json:"payload"`
	Kind    Kind              `json:"kind,omitempty"`
	Hint    string            `json:"hint,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Output is the compressed payload and metadata.
type Output struct {
	Payload        []byte `json:"payload"`
	OriginalSize   int    `json:"original_size"`
	CompressedSize int    `json:"compressed_size"`
	Kind           Kind   `json:"kind"`
	Method         string `json:"method"`
	Error          string `json:"error,omitempty"`
}

// Detector classifies raw payloads into Kind values.
type Detector interface {
	Detect(in Input) (Kind, float64)
}

// Compressor reduces the size of a payload for a specific Kind.
type Compressor interface {
	ID() string
	Compress(kind Kind, payload []byte) ([]byte, error)
}

// DetectFunc is a function adapter for Detector.
type DetectFunc func(in Input) (Kind, float64)

// Detect implements Detector for DetectFunc.
func (f DetectFunc) Detect(in Input) (Kind, float64) {
	return f(in)
}

// Service is the internal headroom service abstraction.
type Service interface {
	Compress(ctx context.Context, in Input) (Output, error)
	Enabled() bool
}
