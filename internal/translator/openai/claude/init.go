package claude

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatOpenAI,
		types.FormatClaude,
		convertOpenAIRequestToClaude,
		types.ResponseTransform{
			Stream:    convertClaudeResponseToOpenAIStream,
			NonStream: convertClaudeResponseToOpenAINonStream,
		},
	)
}
