package claude

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// Kiro → Claude response translation. Kiro responses are OpenAI-compatible,
	// so reuse the existing OpenAI → Claude converters to emit Claude Messages
	// SSE events and non-streaming response objects.
	registry.Register(
		types.FormatKiro,
		types.FormatClaude,
		nil, // request transform lives in translator/claude/kiro
		types.ResponseTransform{
			Stream:    ConvertKiroResponseToClaudeStream,
			NonStream: ConvertKiroResponseToClaudeNonStream,
		},
	)
}
