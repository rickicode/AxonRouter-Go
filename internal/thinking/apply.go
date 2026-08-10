package thinking

import (
	"strings"

	"github.com/tidwall/sjson"
)

// ApplyThinkingOverride injects or overrides thinking configuration in a
// provider-format request body.
//
// Supported target formats:
//   - "claude"                -> thinking.type + thinking.budget_tokens
//   - "gemini"/"antigravity"  -> generationConfig.thinkingConfig.thinkingBudget
//   - "openai"                -> reasoning_effort
//   - "openai-responses"      -> reasoning.effort
//
// Special budget values:
//   - BudgetDisabled (0)      -> removes/disables thinking
//   - BudgetAuto (-1)         -> enables thinking without a fixed budget for
//     budget-style providers; emits "auto" level for
//     level-style providers.
func ApplyThinkingOverride(jsonBody []byte, budget int, targetFormat string) []byte {
	if len(jsonBody) == 0 {
		return jsonBody
	}

	format := strings.ToLower(strings.TrimSpace(targetFormat))
	switch format {
	case "claude":
		return applyClaude(jsonBody, budget)
	case "gemini", "antigravity":
		return applyGemini(jsonBody, budget)
	case "openai":
		return applyOpenAI(jsonBody, budget)
	case "openai-responses", "codex":
		return applyOpenAIResponses(jsonBody, budget)
	default:
		return jsonBody
	}
}

func applyClaude(body []byte, budget int) []byte {
	const pathType = "thinking.type"
	const pathBudget = "thinking.budget_tokens"

	switch {
	case budget <= BudgetDisabled:
		body, _ = sjson.SetBytes(body, pathType, "disabled")
		body, _ = sjson.DeleteBytes(body, pathBudget)
	case budget == BudgetAuto:
		body, _ = sjson.SetBytes(body, pathType, "enabled")
		body, _ = sjson.DeleteBytes(body, pathBudget)
	default:
		body, _ = sjson.SetBytes(body, pathType, "enabled")
		body, _ = sjson.SetBytes(body, pathBudget, budget)
	}
	return body
}

func applyGemini(body []byte, budget int) []byte {
	const pathBudget = "generationConfig.thinkingConfig.thinkingBudget"
	const pathLevel = "generationConfig.thinkingConfig.thinkingLevel"

	switch {
	case budget <= BudgetDisabled:
		body, _ = sjson.SetBytes(body, pathBudget, 0)
		body, _ = sjson.DeleteBytes(body, pathLevel)
	case budget == BudgetAuto:
		body, _ = sjson.DeleteBytes(body, pathBudget)
		body, _ = sjson.DeleteBytes(body, pathLevel)
	default:
		body, _ = sjson.SetBytes(body, pathBudget, budget)
		body, _ = sjson.DeleteBytes(body, pathLevel)
	}
	return body
}

func applyOpenAI(body []byte, budget int) []byte {
	level := budgetToLevel(budget)
	if level == "" {
		body, _ = sjson.DeleteBytes(body, "reasoning_effort")
		return body
	}
	body, _ = sjson.SetBytes(body, "reasoning_effort", level)
	return body
}

func applyOpenAIResponses(body []byte, budget int) []byte {
	level := budgetToLevel(budget)
	if level == "" {
		body, _ = sjson.DeleteBytes(body, "reasoning.effort")
		return body
	}
	body, _ = sjson.SetBytes(body, "reasoning.effort", level)
	return body
}

func budgetToLevel(budget int) string {
	switch {
	case budget < BudgetAuto:
		return ""
	case budget == BudgetAuto:
		return "auto"
	case budget == BudgetDisabled:
		return "none"
	case budget <= 512:
		return "low"
	case budget <= 1024:
		return "low"
	case budget <= 8192:
		return "medium"
	case budget <= 24576:
		return "high"
	default:
		return "high"
	}
}
