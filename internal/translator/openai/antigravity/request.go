package antigravity

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// antigravity → openai (reverse request transform)
	// Antigravity accepts OpenAI-compatible JSON. Passthrough for request direction.
	registry.Register(
		types.FormatAntigravity,
		types.FormatOpenAI,
		antigravityToOpenAIRequest,
		types.ResponseTransform{},
	)
}

// antigravityToOpenAIRequest converts Antigravity request format to OpenAI.
// Since Antigravity accepts OpenAI-compatible JSON, this is a passthrough.
func antigravityToOpenAIRequest(model string, rawJSON []byte, stream bool) []byte {
	return rawJSON
}
