package translator

// This file imports all translator implementations.
// Each translator registers itself via init() in its own package.
// The import uses _ to trigger init() side effects.

import (
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/antigravity/openai"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/claude/openai"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/codex/responses"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/gemini/openai"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/kiro/openai"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/openai/antigravity"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/openai/claude"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/openai/codex_responses"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/openai/gemini"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/openai/kiro"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/openai/openai"
)
