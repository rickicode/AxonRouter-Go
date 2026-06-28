package openai

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatGemini,
		types.FormatOpenAI,
		convertGeminiRequestToOpenAI,
		types.ResponseTransform{
			Stream:    convertOpenAIResponseToGeminiStream,
			NonStream: convertOpenAIResponseToGeminiNonStream,
		},
	)
}
