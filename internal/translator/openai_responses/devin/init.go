package devin

import (
	"context"

	"github.com/rickicode/AxonRouter-Go/internal/translator/openai_responses/openai"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// Request: OpenAI Responses API -> Devin CLI (via OpenAI Chat Completions shape;
	// the Devin CLI executor consumes chat completion messages).
	registry.Register(
		types.FormatCodexResponses,
		types.FormatDevinCLI,
		openai.ConvertOpenAIResponsesRequestToOpenAI,
		types.ResponseTransform{},
	)

	// Response: Devin CLI -> OpenAI Responses API.
	registry.Register(
		types.FormatDevinCLI,
		types.FormatCodexResponses,
		nil,
		types.ResponseTransform{
			Stream:    convertDevinResponseToOpenAIResponsesStream,
			NonStream: convertDevinResponseToOpenAIResponsesNonStream,
		},
	)
}

func convertDevinResponseToOpenAIResponsesStream(ctx context.Context, model string, originalReq, translatedReq, rawChunk []byte, param *any) [][]byte {
	return openai.ConvertOpenAIChatToOpenAIResponsesStream(ctx, model, originalReq, translatedReq, rawChunk, param)
}

func convertDevinResponseToOpenAIResponsesNonStream(ctx context.Context, model string, originalReq, translatedReq, rawResponse []byte, param *any) []byte {
	return openai.ConvertOpenAIChatToOpenAIResponsesNonStream(ctx, model, originalReq, translatedReq, rawResponse, param)
}
