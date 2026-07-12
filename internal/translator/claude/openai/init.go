package openai

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatClaude,
		types.FormatOpenAI,
		ConvertClaudeRequestToOpenAI,
		types.ResponseTransform{
			Stream:    ConvertOpenAIResponseToClaudeStream,
			NonStream: ConvertOpenAIResponseToClaudeNonStream,
		},
	)
}
