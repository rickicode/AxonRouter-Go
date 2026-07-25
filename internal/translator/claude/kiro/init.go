package kiro

import (
	"github.com/rickicode/AxonRouter-Go/internal/translator/registry"
	"github.com/rickicode/AxonRouter-Go/internal/translator/types"
)

func init() {
	// Claude → Kiro request translation. Kiro returns OpenAI-compatible JSON,
	// so the response path back to Claude Messages lives in translator/kiro/claude
	// and reuses the OpenAI → Claude response converter.
	registry.Register(
		types.FormatClaude,
		types.FormatKiro,
		ConvertClaudeRequestToKiro,
		types.ResponseTransform{},
	)
}
