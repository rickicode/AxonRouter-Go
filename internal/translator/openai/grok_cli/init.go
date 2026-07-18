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
}
