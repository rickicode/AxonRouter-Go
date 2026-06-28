package openai

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatClaude,
		types.FormatOpenAI,
		convertClaudeRequestToOpenAI,
		types.ResponseTransform{
			Stream:    convertOpenAIResponseToClaudeStream,
			NonStream: convertOpenAIResponseToClaudeNonStream,
		},
	)
}
