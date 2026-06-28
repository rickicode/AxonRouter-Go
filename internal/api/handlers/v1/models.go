package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Models handles GET /v1/models — includes combos and virtual models.
func (h *Handler) Models(c *gin.Context) {
	prefixes := h.registry.List()
	var models []gin.H

	for _, prefix := range prefixes {
		for _, m := range h.getProviderModels(prefix) {
			models = append(models, m)
		}
	}

	// Add combo names as virtual models
	for _, combo := range h.combo.ListCombos() {
		models = append(models, gin.H{
			"id":       combo.Combo.Name,
			"object":   "model",
			"created":  combo.Combo.CreatedAt,
			"owned_by": "axonrouter",
		})
	}

	// Add virtual/smart models
	virtualModels := []string{"auto", "economy", "balanced", "premium"}
	for _, name := range virtualModels {
		models = append(models, gin.H{
			"id":       "smart/" + name,
			"object":   "model",
			"created":  1700000000,
			"owned_by": "axonrouter",
		})
	}

	if len(models) == 0 {
		models = h.defaultModels()
		models = append(models, mimoModels()...)
		models = append(models, opencodeModels()...)
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   models,
	})
}

// getProviderModels returns models for a provider prefix.
func (h *Handler) getProviderModels(prefix string) []gin.H {
	// ponytail: hardcoded model lists per provider
	// Load from DB or config when dynamic models are needed
	switch prefix {
	case "openai":
		return openaiModels()
	case "claude":
		return claudeModels()
	case "gemini":
		return geminiModels()
	case "cx":
		return codexModels()
	case "ag":
		return antigravityModels()
	case "kiro":
		return kiroModels()
	case "groq":
		return groqModels()
	case "deepseek":
		return deepseekModels()
	case "mimo", "mimocode", "mimo-tp":
		return mimoModels()
	case "oc", "oc-zen", "oc-go":
		return opencodeModels()
	default:
		return nil
	}
}

func openaiModels() []gin.H {
	return []gin.H{
		{"id": "openai/gpt-4o", "object": "model", "created": 1700000000, "owned_by": "openai"},
		{"id": "openai/gpt-4o-mini", "object": "model", "created": 1700000000, "owned_by": "openai"},
		{"id": "openai/gpt-4-turbo", "object": "model", "created": 1700000000, "owned_by": "openai"},
		{"id": "openai/o1", "object": "model", "created": 1700000000, "owned_by": "openai"},
		{"id": "openai/o1-mini", "object": "model", "created": 1700000000, "owned_by": "openai"},
		{"id": "openai/o1-pro", "object": "model", "created": 1700000000, "owned_by": "openai"},
		{"id": "openai/o3", "object": "model", "created": 1700000000, "owned_by": "openai"},
		{"id": "openai/o3-mini", "object": "model", "created": 1700000000, "owned_by": "openai"},
		{"id": "openai/o4-mini", "object": "model", "created": 1700000000, "owned_by": "openai"},
		{"id": "openai/gpt-4.1", "object": "model", "created": 1700000000, "owned_by": "openai"},
		{"id": "openai/gpt-4.1-mini", "object": "model", "created": 1700000000, "owned_by": "openai"},
		{"id": "openai/gpt-4.1-nano", "object": "model", "created": 1700000000, "owned_by": "openai"},
	}
}

func claudeModels() []gin.H {
	return []gin.H{
		{"id": "claude/claude-sonnet-4-20250514", "object": "model", "created": 1700000000, "owned_by": "anthropic"},
		{"id": "claude/claude-opus-4-20250514", "object": "model", "created": 1700000000, "owned_by": "anthropic"},
		{"id": "claude/claude-3-5-sonnet-20241022", "object": "model", "created": 1700000000, "owned_by": "anthropic"},
		{"id": "claude/claude-3-5-haiku-20241022", "object": "model", "created": 1700000000, "owned_by": "anthropic"},
		{"id": "claude/claude-3-opus-20240229", "object": "model", "created": 1700000000, "owned_by": "anthropic"},
	}
}

func geminiModels() []gin.H {
	return []gin.H{
		{"id": "gemini/gemini-2.5-pro-preview-05-06", "object": "model", "created": 1700000000, "owned_by": "google"},
		{"id": "gemini/gemini-2.5-flash-preview-05-20", "object": "model", "created": 1700000000, "owned_by": "google"},
		{"id": "gemini/gemini-2.0-flash", "object": "model", "created": 1700000000, "owned_by": "google"},
		{"id": "gemini/gemini-1.5-pro", "object": "model", "created": 1700000000, "owned_by": "google"},
		{"id": "gemini/gemini-1.5-flash", "object": "model", "created": 1700000000, "owned_by": "google"},
	}
}

func codexModels() []gin.H {
	return []gin.H{
		{"id": "cx/gpt-5.4", "object": "model", "created": 1700000000, "owned_by": "openai"},
		{"id": "cx/gpt-5.4-mini", "object": "model", "created": 1700000000, "owned_by": "openai"},
		{"id": "cx/o3", "object": "model", "created": 1700000000, "owned_by": "openai"},
		{"id": "cx/o4-mini", "object": "model", "created": 1700000000, "owned_by": "openai"},
		{"id": "cx/codex-mini", "object": "model", "created": 1700000000, "owned_by": "openai"},
	}
}

func antigravityModels() []gin.H {
	return []gin.H{
		{"id": "ag/gemini-2.5-pro", "object": "model", "created": 1700000000, "owned_by": "google"},
		{"id": "ag/gemini-2.5-flash", "object": "model", "created": 1700000000, "owned_by": "google"},
	}
}

func kiroModels() []gin.H {
	return []gin.H{
		{"id": "kiro/claude-sonnet-4", "object": "model", "created": 1700000000, "owned_by": "aws"},
		{"id": "kiro/claude-3-5-sonnet", "object": "model", "created": 1700000000, "owned_by": "aws"},
	}
}

func groqModels() []gin.H {
	return []gin.H{
		{"id": "groq/llama-3.3-70b-versatile", "object": "model", "created": 1700000000, "owned_by": "groq"},
		{"id": "groq/llama-3.1-8b-instant", "object": "model", "created": 1700000000, "owned_by": "groq"},
		{"id": "groq/mixtral-8x7b-32768", "object": "model", "created": 1700000000, "owned_by": "groq"},
	}
}

func deepseekModels() []gin.H {
	return []gin.H{
		{"id": "deepseek/deepseek-chat", "object": "model", "created": 1700000000, "owned_by": "deepseek"},
		{"id": "deepseek/deepseek-reasoner", "object": "model", "created": 1700000000, "owned_by": "deepseek"},
	}
}

func (h *Handler) defaultModels() []gin.H {
	var all []gin.H
	all = append(all, openaiModels()...)
	all = append(all, claudeModels()...)
	all = append(all, geminiModels()...)
	all = append(all, codexModels()...)
	all = append(all, antigravityModels()...)
	all = append(all, kiroModels()...)
	all = append(all, mimoModels()...)
	all = append(all, opencodeModels()...)
	return all
}

func mimoModels() []gin.H {
	return []gin.H{
		{"id": "mimo/mimo-v2.5-pro", "object": "model", "created": 1700000000, "owned_by": "xiaomi"},
		{"id": "mimo/mimo-v2.5", "object": "model", "created": 1700000000, "owned_by": "xiaomi"},
		{"id": "mimo/mimo-v2-pro", "object": "model", "created": 1700000000, "owned_by": "xiaomi"},
		{"id": "mimo/mimo-v2-omni", "object": "model", "created": 1700000000, "owned_by": "xiaomi"},
		{"id": "mimo/mimo-v2-flash", "object": "model", "created": 1700000000, "owned_by": "xiaomi"},
	}
}

func opencodeModels() []gin.H {
	return []gin.H{
		{"id": "oc/kimi-k2", "object": "model", "created": 1700000000, "owned_by": "opencode"},
		{"id": "oc/glm-4", "object": "model", "created": 1700000000, "owned_by": "opencode"},
		{"id": "oc/qwen-2.5-72b", "object": "model", "created": 1700000000, "owned_by": "opencode"},
	}
}
