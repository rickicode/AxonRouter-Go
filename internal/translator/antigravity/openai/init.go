package openai

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatOpenAI,
		types.FormatAntigravity,
		convertOpenAIRequestToAntigravity,
		types.ResponseTransform{
			Stream:    convertAntigravityResponseToOpenAIStream,
			NonStream: convertAntigravityResponseToOpenAINonStream,
		},
	)
}
