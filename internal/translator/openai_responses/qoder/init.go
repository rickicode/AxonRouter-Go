package qoder

import (
	"context"

	"github.com/rickicode/AxonRouter-Go/internal/translator/openai_responses/openai"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// Request: OpenAI Responses API -> Qoder (via OpenAI Chat Completions shape;
	// the Qoder executor consumes chat completion messages for both PAT and HTTP paths).
	registry.Register(
		types.FormatCodexResponses,
		types.FormatQoder,
		openai.ConvertOpenAIResponsesRequestToOpenAI,
		types.ResponseTransform{},
	)

	// Response: Qoder -> OpenAI Responses API.
	registry.Register(
		types.FormatQoder,
		types.FormatCodexResponses,
		nil,
		types.ResponseTransform{
			Stream:    convertQoderResponseToOpenAIResponsesStream,
			NonStream: convertQoderResponseToOpenAIResponsesNonStream,
		},
	)
}

func convertQoderResponseToOpenAIResponsesStream(ctx context.Context, model string, originalReq, translatedReq, rawChunk []byte, param *any) [][]byte {
	return openai.ConvertOpenAIChatToOpenAIResponsesStream(ctx, model, originalReq, translatedReq, rawChunk, param)
}

func convertQoderResponseToOpenAIResponsesNonStream(ctx context.Context, model string, originalReq, translatedReq, rawResponse []byte, param *any) []byte {
	return openai.ConvertOpenAIChatToOpenAIResponsesNonStream(ctx, model, originalReq, translatedReq, rawResponse, param)
}
