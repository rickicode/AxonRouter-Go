package antigravity

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/claude/openai"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatClaude,
		types.FormatAntigravity,
		openai.ConvertClaudeRequestToOpenAI,
		types.ResponseTransform{
			Stream:    openai.ConvertOpenAIResponseToClaudeStream,
			NonStream: openai.ConvertOpenAIResponseToClaudeNonStream,
		},
	)
}
