package gemini

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatCodexResponses,
		types.FormatGemini,
		convertCodexRequestToGemini,
		types.ResponseTransform{
			Stream:    convertGeminiResponseToCodexStream,
			NonStream: convertGeminiResponseToCodexNonStream,
		},
	)
}
