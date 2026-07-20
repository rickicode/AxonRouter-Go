package claude

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatKiro,
		types.FormatClaude,
		nil, // request transform lives in translator/claude/kiro
		types.ResponseTransform{
			Stream: ConvertKiroResponseToClaudeStream,
		},
	)
}
