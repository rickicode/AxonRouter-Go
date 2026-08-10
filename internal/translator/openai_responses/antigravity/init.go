package antigravity

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/openai_responses/gemini"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// Request: OpenAI Responses API -> Antigravity (Gemini generateContent wrapped by executor).
	registry.Register(
		types.FormatCodexResponses,
		types.FormatAntigravity,
		convertOpenAIResponsesRequestToAntigravity,
		types.ResponseTransform{},
	)

	// Response: Antigravity -> OpenAI Responses API.
	registry.Register(
		types.FormatAntigravity,
		types.FormatCodexResponses,
		nil,
		types.ResponseTransform{
			Stream:    convertAntigravityResponseToOpenAIResponsesStream,
			NonStream: convertAntigravityResponseToOpenAIResponsesNonStream,
		},
	)
}

// convertOpenAIResponsesRequestToAntigravity reuses the Gemini request translator;
// the Antigravity executor wraps the generated Gemini contents into its envelope.
func convertOpenAIResponsesRequestToAntigravity(model string, body []byte, stream bool) []byte {
	return gemini.ConvertOpenAIResponsesRequestToGemini(model, body, stream)
}
