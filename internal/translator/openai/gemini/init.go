package gemini

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatOpenAI,
		types.FormatGemini,
		convertOpenAIRequestToGemini,
		types.ResponseTransform{
			Stream:    convertGeminiResponseToOpenAIStream,
			NonStream: convertGeminiResponseToOpenAINonStream,
		},
	)
}
