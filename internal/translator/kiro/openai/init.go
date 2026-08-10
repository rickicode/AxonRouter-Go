package openai

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatOpenAI,
		types.FormatKiro,
		nil, // request is handled by translator/openai/kiro
		types.ResponseTransform{
			Stream:    convertKiroResponseToOpenAIStream,
			NonStream: convertKiroResponseToOpenAINonStream,
		},
	)
}
