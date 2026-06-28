package responses

import (
	"context"

	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatCodexResponses,
		types.FormatOpenAI,
		nil, // passthrough: Codex uses OpenAI Responses format natively
		types.ResponseTransform{
			Stream:    passthroughStream,
			NonStream: passthroughNonStream,
		},
	)
}

func passthroughStream(_ context.Context, _ string, _, _, rawChunk []byte, _ *any) [][]byte {
	return [][]byte{rawChunk}
}

func passthroughNonStream(_ context.Context, _ string, _, _, rawResponse []byte, _ *any) []byte {
	return rawResponse
}
