package kiro

import (
	"context"

	"github.com/rickicode/AxonRouter-Go/internal/translator/openai_responses/openai"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// Request: OpenAI Responses API -> Kiro (via OpenAI Chat Completions shape;
	// the Kiro executor performs the final Chat Completions -> Kiro conversion).
	registry.Register(
		types.FormatCodexResponses,
		types.FormatKiro,
		openai.ConvertOpenAIResponsesRequestToOpenAI,
		types.ResponseTransform{},
	)

	// Response: Kiro -> OpenAI Responses API.
	registry.Register(
		types.FormatKiro,
		types.FormatCodexResponses,
		nil,
		types.ResponseTransform{
			Stream:    convertKiroResponseToOpenAIResponsesStream,
			NonStream: convertKiroResponseToOpenAIResponsesNonStream,
		},
	)
}

// convertKiroResponseToOpenAIResponsesStream converts Kiro's native OpenAI Chat
// Completions SSE stream back to the OpenAI Responses API streaming shape.
func convertKiroResponseToOpenAIResponsesStream(ctx context.Context, model string, originalReq, translatedReq, rawChunk []byte, param *any) [][]byte {
	return openai.ConvertOpenAIChatToOpenAIResponsesStream(ctx, model, originalReq, translatedReq, rawChunk, param)
}

// convertKiroResponseToOpenAIResponsesNonStream converts Kiro's native OpenAI Chat
// Completions JSON response back to the OpenAI Responses API non-streaming shape.
func convertKiroResponseToOpenAIResponsesNonStream(ctx context.Context, model string, originalReq, translatedReq, rawResponse []byte, param *any) []byte {
	return openai.ConvertOpenAIChatToOpenAIResponsesNonStream(ctx, model, originalReq, translatedReq, rawResponse, param)
}
