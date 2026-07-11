package openai

import (
	"context"

	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatOpenAI,
		types.FormatKiro,
		nil, // Kiro uses OpenAI format natively, passthrough
		types.ResponseTransform{
			Stream:    passthroughStream,
			NonStream: passthroughNonStream,
		},
	)
}

func passthroughStream(_ context.Context, _ string, _, _, rawChunk []byte, _ *any) [][]byte {
	return [][]byte{append(rawChunk, "\n\n"...)}
}

func passthroughNonStream(_ context.Context, _ string, _, _, rawResponse []byte, _ *any) []byte {
	return rawResponse
}
