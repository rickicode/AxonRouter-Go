package translator

// This file imports all translator implementations.
// Each translator registers itself via init() in its own package.
// The import uses _ to trigger init() side effects.
//
// Import order matters: registry.Register is last-write-wins, so later imports
// override earlier ones for the same (from, to) pair. Keep the more specific
// Responses API translators (openai_responses/...) after the legacy codex/* ones
// so that the Responses-specific implementations win for
// (openai-responses, claude) and (openai-responses, gemini).
//
// Documented response mappings:
//   - Antigravity (Gemini Cloud Code Assist envelopes) -> Claude Messages:
//     internal/translator/antigravity/claude
//   - Kiro (OpenAI-compatible chat.completions) -> Claude Messages:
//     internal/translator/kiro/claude (reuses the OpenAI -> Claude converter)
//
// Intentionally omitted: openai/openai_responses. The generic translator would
// forward fields rejected by the Codex upstream (e.g. max_tokens, temperature);
// the Codex path uses openai/codex_responses for requests and codex/responses
// for responses instead.

import (
	// Existing translators
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/antigravity/openai"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/claude/antigravity"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/claude/kiro"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/claude/openai"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/gemini/openai"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/kiro/claude"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/kiro/openai"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/openai/antigravity"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/openai/claude"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/openai/gemini"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/openai/kiro"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/openai/openai"

	// NEW — 7 additional translator pairs. These are ordered so that the
	// openai_responses/claude and openai_responses/gemini registrations below
	// take precedence over the legacy codex/claude and codex/gemini packages for
	// the Responses API surface.
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/antigravity/claude"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/antigravity/gemini"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/claude/gemini"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/codex/claude"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/codex/gemini"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/gemini/claude"

	// Codex (Responses API): request transform in openai/codex_responses,
	// response transform in codex/responses. The generic openai/openai_responses
	// translator is intentionally omitted because it is not Codex-compatible.
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/codex/responses"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/openai/codex_responses"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/openai/grok_cli"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/openai_responses/claude"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/openai_responses/gemini"
	_ "github.com/rickicode/AxonRouter-Go/internal/translator/openai_responses/openai"
)
