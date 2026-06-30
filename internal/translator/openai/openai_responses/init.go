package openai_responses

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatOpenAI,
		types.FormatCodexResponses,
		convertOpenAIRequestToCodexResponses,
		types.ResponseTransform{
			Stream:    convertCodexResponsesToOpenAIStream,
			NonStream: convertCodexResponsesToOpenAINonStream,
		},
	)
}
