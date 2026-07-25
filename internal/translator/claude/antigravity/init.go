package antigravity

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatClaude,
		types.FormatAntigravity,
		convertClaudeRequestToAntigravity,
		types.ResponseTransform{},
	)
}
