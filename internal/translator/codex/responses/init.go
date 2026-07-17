package responses

import (
	"context"

	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// Codex Responses → OpenAI Chat Completions response translation.
	// The handler's streaming path looks up (clientFormat, providerFormat), i.e.
	// (openai, openai-responses), while the legacy non-stream /v1/chat/completions
	// path looks up (providerFormat, clientFormat). Register both directions so
	// the same Codex-specific response transform is used regardless of call site.
	resp := types.ResponseTransform{
		Stream:    convertCodexResponseToOpenAIStream,
		NonStream: convertCodexResponseToOpenAINonStream,
	}
	registry.Register(
		types.FormatOpenAI,
		types.FormatCodexResponses,
		nil,
		resp,
	)
	registry.Register(
		types.FormatCodexResponses,
		types.FormatOpenAI,
		nil,
		resp,
	)
}

func passthroughStream(_ context.Context, _ string, _, _, rawChunk []byte, _ *any) [][]byte {
	return [][]byte{rawChunk}
}

func passthroughNonStream(_ context.Context, _ string, _, _, rawResponse []byte, _ *any) []byte {
	return rawResponse
}
