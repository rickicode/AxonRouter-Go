package antigravity

import (
	"context"

	"github.com/rickicode/AxonRouter-Go/internal/translator/openai_responses/openai"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

// streamState holds the per-stream state for both the Antigravity -> OpenAI
// Chat Completions stage and the Chat Completions -> OpenAI Responses API stage.
type streamState struct {
	inner any
	oai   any
}

func getStreamState(param *any) *streamState {
	if *param == nil {
		*param = &streamState{}
	}
	return (*param).(*streamState)
}

// convertAntigravityResponseToOpenAIResponsesStream chains the registered
// Antigravity -> OpenAI Chat Completions response translator with the
// OpenAI Chat Completions -> OpenAI Responses API translator.
func convertAntigravityResponseToOpenAIResponsesStream(ctx context.Context, model string, originalReq, translatedReq, rawChunk []byte, param *any) [][]byte {
	state := getStreamState(param)
	chatChunks := registry.Response(ctx, string(types.FormatAntigravity), string(types.FormatOpenAI), model, originalReq, translatedReq, rawChunk, &state.inner)
	if len(chatChunks) == 0 {
		return nil
	}

	var out [][]byte
	for _, chatChunk := range chatChunks {
		out = append(out, openai.ConvertOpenAIChatToOpenAIResponsesStream(ctx, model, originalReq, translatedReq, chatChunk, &state.oai)...)
	}
	return out
}

// convertAntigravityResponseToOpenAIResponsesNonStream chains the registered
// Antigravity -> OpenAI Chat Completions response translator with the
// OpenAI Chat Completions -> OpenAI Responses API translator.
func convertAntigravityResponseToOpenAIResponsesNonStream(ctx context.Context, model string, originalReq, translatedReq, rawResponse []byte, param *any) []byte {
	chatBody := registry.ResponseNonStream(ctx, string(types.FormatAntigravity), string(types.FormatOpenAI), model, originalReq, translatedReq, rawResponse, param)
	if len(chatBody) == 0 {
		return rawResponse
	}
	return openai.ConvertOpenAIChatToOpenAIResponsesNonStream(ctx, model, originalReq, translatedReq, chatBody, nil)
}
