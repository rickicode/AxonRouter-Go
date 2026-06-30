package claude

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatGemini,
		types.FormatClaude,
		convertGeminiRequestToClaude,
		types.ResponseTransform{
			Stream:    convertClaudeResponseToGeminiStream,
			NonStream: convertClaudeResponseToGeminiNonStream,
		},
	)
}
