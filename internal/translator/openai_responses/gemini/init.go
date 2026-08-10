package gemini

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// Request: OpenAI Responses API → Gemini generateContent.
	registry.Register(
		types.FormatCodexResponses,
		types.FormatGemini,
		ConvertOpenAIResponsesRequestToGemini,
		types.ResponseTransform{},
	)

	// Response: Gemini → OpenAI Responses API (bidirectional registration).
	resp := types.ResponseTransform{
		Stream:    convertGeminiResponseToOpenAIResponsesStream,
		NonStream: convertGeminiResponseToOpenAIResponsesNonStream,
	}
	registry.Register(
		types.FormatGemini,
		types.FormatCodexResponses,
		nil,
		resp,
	)
	registry.Register(
		types.FormatCodexResponses,
		types.FormatGemini,
		nil,
		resp,
	)
}
