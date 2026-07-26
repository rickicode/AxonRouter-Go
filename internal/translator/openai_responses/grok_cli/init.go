package grok_cli

import (
	"context"

	"github.com/rickicode/AxonRouter-Go/internal/translator/openai_responses/openai"
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// Request: OpenAI Responses API -> Grok CLI. The Grok CLI executor expects a
	// Responses API payload, so the translator is a passthrough.
	registry.Register(
		types.FormatCodexResponses,
		types.FormatGrokCLI,
		convertOpenAIResponsesRequestToGrokCLI,
		types.ResponseTransform{},
	)

	// Response: Grok CLI -> OpenAI Responses API.
	registry.Register(
		types.FormatGrokCLI,
		types.FormatCodexResponses,
		nil,
		types.ResponseTransform{
			Stream:    convertGrokCLIResponseToOpenAIResponsesStream,
			NonStream: convertGrokCLIResponseToOpenAIResponsesNonStream,
		},
	)
}

// convertOpenAIResponsesRequestToGrokCLI passes the Responses API body through;
// the Grok CLI executor performs its own normalization.
func convertOpenAIResponsesRequestToGrokCLI(_ string, body []byte, _ bool) []byte {
	return body
}

// streamState holds the per-stream state for both the Grok CLI -> OpenAI
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

// convertGrokCLIResponseToOpenAIResponsesStream chains the registered
// Grok CLI -> OpenAI Chat Completions response translator with the
// OpenAI Chat Completions -> OpenAI Responses API translator.
func convertGrokCLIResponseToOpenAIResponsesStream(ctx context.Context, model string, originalReq, translatedReq, rawChunk []byte, param *any) [][]byte {
	state := getStreamState(param)
	chatChunks := registry.Response(ctx, string(types.FormatGrokCLI), string(types.FormatOpenAI), model, originalReq, translatedReq, rawChunk, &state.inner)
	if len(chatChunks) == 0 {
		return nil
	}

	var out [][]byte
	for _, chatChunk := range chatChunks {
		out = append(out, openai.ConvertOpenAIChatToOpenAIResponsesStream(ctx, model, originalReq, translatedReq, chatChunk, &state.oai)...)
	}
	return out
}

// convertGrokCLIResponseToOpenAIResponsesNonStream chains the registered
// Grok CLI -> OpenAI Chat Completions response translator with the
// OpenAI Chat Completions -> OpenAI Responses API translator.
func convertGrokCLIResponseToOpenAIResponsesNonStream(ctx context.Context, model string, originalReq, translatedReq, rawResponse []byte, param *any) []byte {
	chatBody := registry.ResponseNonStream(ctx, string(types.FormatGrokCLI), string(types.FormatOpenAI), model, originalReq, translatedReq, rawResponse, param)
	if len(chatBody) == 0 {
		return rawResponse
	}
	return openai.ConvertOpenAIChatToOpenAIResponsesNonStream(ctx, model, originalReq, translatedReq, chatBody, nil)
}
