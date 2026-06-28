// Package types defines translator types used across the codebase.
// This package has no dependencies on implementation packages.
package types

import "context"

// Format identifies a request/response schema used inside the proxy.
type Format string

const (
	FormatOpenAI         Format = "openai"
	FormatClaude         Format = "claude"
	FormatGemini         Format = "gemini"
	FormatCodexResponses Format = "codex-responses"
	FormatAntigravity    Format = "antigravity"
	FormatKiro           Format = "kiro"
)

// TranslateFunc translates a request body from one format to another.
type TranslateFunc func(model string, body []byte, stream bool) []byte

// TranslateStreamFunc translates a streaming response chunk.
type TranslateStreamFunc func(ctx context.Context, model string, originalReq, translatedReq, rawChunk []byte, param *any) [][]byte

// TranslateNonStreamFunc translates a non-streaming response body.
type TranslateNonStreamFunc func(ctx context.Context, model string, originalReq, translatedReq, rawResponse []byte, param *any) []byte

// ResponseTransform holds both stream and non-stream response translators.
type ResponseTransform struct {
	Stream    TranslateStreamFunc
	NonStream TranslateNonStreamFunc
}
