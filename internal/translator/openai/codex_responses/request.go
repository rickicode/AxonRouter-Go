package codex_responses

import (
	"context"

	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// openai → codex-responses (reverse: register request transform)
	registry.Register(
		types.FormatOpenAI,
		types.FormatCodexResponses,
		openaiToCodexRequest,
		types.ResponseTransform{
			Stream:    passthroughStream,
			NonStream: passthroughNonStream,
		},
	)
}

// openaiToCodexRequest converts OpenAI chat format to Codex Responses format.
// The body is passed through as-is since Codex accepts OpenAI-compatible JSON.
func openaiToCodexRequest(model string, rawJSON []byte, stream bool) []byte {
	return rawJSON
}

func passthroughStream(_ context.Context, _ string, _, _, rawChunk []byte, _ *any) [][]byte {
	return [][]byte{rawChunk}
}

func passthroughNonStream(_ context.Context, _ string, _, _, rawResponse []byte, _ *any) []byte {
	return rawResponse
}
