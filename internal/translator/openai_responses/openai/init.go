package openai

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// Request: OpenAI Responses API -> OpenAI Chat Completions.
	registry.Register(
		types.FormatCodexResponses,
		types.FormatOpenAI,
		convertResponsesRequestToOpenAI,
		types.ResponseTransform{},
	)

	// Response: OpenAI Chat Completions -> OpenAI Responses API.
	resp := types.ResponseTransform{
		Stream:    convertOpenAIStreamToResponses,
		NonStream: convertOpenAINonStreamToResponses,
	}
	registry.Register(
		types.FormatOpenAI,
		types.FormatCodexResponses,
		nil,
		resp,
	)
}
