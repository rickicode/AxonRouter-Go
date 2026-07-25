package openai

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// OpenAI Responses API request -> OpenAI Chat Completions request.
	registry.Register(
		types.FormatCodexResponses,
		types.FormatOpenAI,
		convertOpenAIResponsesRequestToOpenAI,
		types.ResponseTransform{},
	)
}
