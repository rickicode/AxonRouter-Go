package kiro

import (
	"strings"

	claudeopenai "github.com/rickicode/AxonRouter-Go/internal/translator/claude/openai"
	openaikiro "github.com/rickicode/AxonRouter-Go/internal/translator/openai/kiro"
)

// ConvertClaudeRequestToKiro translates an Anthropic Messages request into a
// Kiro generateAssistantResponse payload. It reuses the Claude→OpenAI and
// OpenAI→Kiro translators so that message blocks (text, image, tool_use,
// tool_result), system prompts, and adaptive-thinking handling are all
// preserved without duplicating logic.
func ConvertClaudeRequestToKiro(model string, body []byte, stream bool) []byte {
	// Normalize a possible provider prefix so downstream model IDs match Kiro's
	// expected shape (e.g. cx/claude-sonnet-4-6 -> claude-sonnet-4-6).
	if i := strings.IndexByte(model, '/'); i >= 0 {
		model = model[i+1:]
	}

	openaiBody := claudeopenai.ConvertClaudeRequestToOpenAI(model, body, stream)
	return openaikiro.ConvertOpenAIRequestToKiro(model, openaiBody, stream)
}
