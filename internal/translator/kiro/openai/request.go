package openai

import (
	"github.com/rickicode/AxonRouter-Go/internal/headroom"
	"github.com/rickicode/AxonRouter-Go/internal/translator/common"
	"github.com/tidwall/sjson"
)

// convertOpenAIRequestToKiro converts an OpenAI Chat Completions request to Kiro format.
// Kiro uses OpenAI format natively, so this is a passthrough with model name normalization.
func convertOpenAIRequestToKiro(modelName string, body []byte, _ bool) []byte {
	body = common.CompressToolBlocks(body, headroom.GlobalToolCompressor(), headroom.DefaultToolThreshold)
	// Kiro uses OpenAI format natively, just normalize model name
	body, _ = sjson.SetBytes(body, "model", modelName)
	return body
}
