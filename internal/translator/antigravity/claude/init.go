package claude

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// Antigravity → Claude response transform. Used when a Claude-format client
	// calls an Antigravity provider and the upstream returns Gemini/Antigravity
	// shaped payloads.
	registry.Register(
		types.FormatAntigravity,
		types.FormatClaude,
		nil,
		types.ResponseTransform{
			Stream:    convertAntigravityResponseToClaudeStream,
			NonStream: convertAntigravityResponseToClaudeNonStream,
		},
	)
}
