package interactions

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// OpenAI chat-completions client -> Interactions provider.
	registry.Register(
		types.FormatOpenAI,
		types.FormatInteractions,
		convertOpenAIRequestToInteractions,
		types.ResponseTransform{
			Stream:    convertInteractionsResponseToOpenAIStream,
			NonStream: convertInteractionsResponseToOpenAINonStream,
		},
	)
	// Interactions client -> OpenAI chat-completions provider.
	registry.Register(
		types.FormatInteractions,
		types.FormatOpenAI,
		convertInteractionsRequestToOpenAI,
		types.ResponseTransform{
			Stream:    convertOpenAIResponseToInteractionsStream,
			NonStream: convertOpenAIResponseToInteractionsNonStream,
		},
	)
}
