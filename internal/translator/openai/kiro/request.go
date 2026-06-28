package kiro

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// kiro → openai (reverse request transform)
	// Kiro uses OpenAI-compatible format. Passthrough for request direction.
	registry.Register(
		types.FormatKiro,
		types.FormatOpenAI,
		kiroToOpenAIRequest,
		types.ResponseTransform{},
	)
}

// kiroToOpenAIRequest converts Kiro request format to OpenAI.
// Kiro accepts OpenAI-compatible JSON, so this is a passthrough.
func kiroToOpenAIRequest(model string, rawJSON []byte, stream bool) []byte {
	return rawJSON
}
