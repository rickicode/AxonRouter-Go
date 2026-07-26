package interactions

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// OpenAI chat-completions client -> Interactions provider.
	// The response transform is looked up by (providerFormat, clientFormat),
	// so for provider=interactions and client=openai the stored function must
	// convert Interactions response -> OpenAI chat-completions.
	registry.Register(
		types.FormatOpenAI,
		types.FormatInteractions,
		convertOpenAIRequestToInteractions,
		types.ResponseTransform{
			Stream:    convertOpenAIResponseToInteractionsStream,
			NonStream: convertOpenAIResponseToInteractionsNonStream,
		},
	)
	// Interactions client -> OpenAI chat-completions provider.
	// For provider=openai and client=interactions, the response transform must
	// convert OpenAI response -> Interactions.
	registry.Register(
		types.FormatInteractions,
		types.FormatOpenAI,
		convertInteractionsRequestToOpenAI,
		types.ResponseTransform{
			Stream:    convertInteractionsResponseToOpenAIStream,
			NonStream: convertInteractionsResponseToOpenAINonStream,
		},
	)
}
