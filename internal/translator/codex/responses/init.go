package responses

import (
	"context"

	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// codex-responses → openai: response translation only (no request transform needed here)
	registry.Register(
		types.FormatCodexResponses,
		types.FormatOpenAI,
		nil, // no request transform: response passthrough path
		types.ResponseTransform{
			Stream:    convertCodexResponseToOpenAIStream,
			NonStream: convertCodexResponseToOpenAINonStream,
		},
	)
}

func passthroughStream(_ context.Context, _ string, _, _, rawChunk []byte, _ *any) [][]byte {
	return [][]byte{rawChunk}
}

func passthroughNonStream(_ context.Context, _ string, _, _, rawResponse []byte, _ *any) []byte {
	return rawResponse
}
