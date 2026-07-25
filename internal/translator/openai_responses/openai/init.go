package openai

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// Response: OpenAI Chat Completions -> OpenAI Responses API.
	registry.Register(
		types.FormatOpenAI,
		types.FormatCodexResponses,
		nil,
		types.ResponseTransform{
			Stream:    convertOpenAIChatToOpenAIResponsesStream,
			NonStream: convertOpenAIChatToOpenAIResponsesNonStream,
		},
	)
}
