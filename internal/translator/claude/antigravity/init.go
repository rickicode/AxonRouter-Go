package antigravity

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// Claude → Antigravity (Gemini Cloud Code Assist) request translation.
	// Antigravity returns native Gemini/Antigravity envelopes, so the response
	// path back to Claude Messages lives in translator/antigravity/claude.
	registry.Register(
		types.FormatClaude,
		types.FormatAntigravity,
		convertClaudeRequestToAntigravity,
		types.ResponseTransform{
			Stream:    convertAntigravityResponseToClaudeStream,
			NonStream: convertAntigravityResponseToClaudeNonStream,
		},
	)
}
