package grok_cli

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatOpenAI,
		types.FormatGrokCLI,
		ConvertOpenAIRequestToGrokCLI,
		types.ResponseTransform{
			Stream:    convertGrokResponseToOpenAIStream,
			NonStream: convertGrokResponseToOpenAINonStream,
		},
	)

	// Register the reverse response direction so ChatCompletions can translate
	// Grok CLI Responses upstream bodies back to OpenAI chat completion format.
	registry.Register(
		types.FormatGrokCLI,
		types.FormatOpenAI,
		nil,
		types.ResponseTransform{
			Stream:    convertGrokResponseToOpenAIStream,
			NonStream: convertGrokResponseToOpenAINonStream,
		},
	)
}
