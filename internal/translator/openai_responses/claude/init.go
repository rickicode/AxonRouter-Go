package claude

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// OpenAI Responses API request -> Claude Messages API request.
	registry.Register(
		types.FormatCodexResponses,
		types.FormatClaude,
		ConvertOpenAIResponsesRequestToClaude,
		types.ResponseTransform{},
	)

	// Claude Messages API response -> OpenAI Responses API response.
	resp := types.ResponseTransform{
		Stream:    ConvertClaudeResponseToOpenAIResponses,
		NonStream: ConvertClaudeResponseToOpenAIResponsesNonStream,
	}
	registry.Register(types.FormatClaude, types.FormatCodexResponses, nil, resp)
}
