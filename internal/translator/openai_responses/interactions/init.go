package interactions

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// OpenAI Responses API request -> Google Interactions API request.
	registry.Register(
		types.FormatCodexResponses,
		types.FormatInteractions,
		ConvertOpenAIResponsesRequestToInteractions,
		types.ResponseTransform{},
	)

	// Google Interactions API response -> OpenAI Responses API response.
	resp := types.ResponseTransform{
		Stream:    ConvertInteractionsResponseToOpenAIResponses,
		NonStream: ConvertInteractionsResponseToOpenAIResponsesNonStream,
	}
	registry.Register(types.FormatInteractions, types.FormatCodexResponses, nil, resp)
}
