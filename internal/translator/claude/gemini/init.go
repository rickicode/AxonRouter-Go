package gemini

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatClaude,
		types.FormatGemini,
		convertClaudeRequestToGemini,
		types.ResponseTransform{
			Stream:    convertGeminiResponseToClaudeStream,
			NonStream: convertGeminiResponseToClaudeNonStream,
		},
	)
}
