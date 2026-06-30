package claude

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatCodexResponses,
		types.FormatClaude,
		convertCodexRequestToClaude,
		types.ResponseTransform{
			Stream:    convertClaudeResponseToCodexStream,
			NonStream: convertClaudeResponseToCodexNonStream,
		},
	)
}
