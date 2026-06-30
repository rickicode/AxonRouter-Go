package claude

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatAntigravity,
		types.FormatClaude,
		convertAntigravityRequestToClaude,
		types.ResponseTransform{
			Stream:    convertClaudeResponseToAntigravityStream,
			NonStream: convertClaudeResponseToAntigravityNonStream,
		},
	)
}
