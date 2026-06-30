package gemini

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	registry.Register(
		types.FormatAntigravity,
		types.FormatGemini,
		convertAntigravityRequestToGemini,
		types.ResponseTransform{
			Stream:    convertGeminiResponseToAntigravityStream,
			NonStream: convertGeminiResponseToAntigravityNonStream,
		},
	)
}
