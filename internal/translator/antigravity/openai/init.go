package openai

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	resp := types.ResponseTransform{
		Stream:    convertAntigravityResponseToOpenAIStream,
		NonStream: convertAntigravityResponseToOpenAINonStream,
	}
	// Stream path: handler looks up (clientFormat, providerFormat) = (openai, antigravity).
	registry.Register(
		types.FormatOpenAI,
		types.FormatAntigravity,
		convertOpenAIRequestToAntigravity,
		resp,
	)
	// Non-stream /v1/chat/completions path looks up (providerFormat, clientFormat) =
	// (antigravity, openai). Register the same response transform there too.
	registry.Register(
		types.FormatAntigravity,
		types.FormatOpenAI,
		nil,
		resp,
	)
}
