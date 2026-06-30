package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/models"
)

// v1ProviderCatalog maps provider prefix → models.json keys + owned_by.
// Providers not listed here (openai, groq, deepseek, mimo, opencode, etc.)
// have no static catalog — their models are discovered dynamically from upstream.
var v1ProviderCatalog = map[string]struct {
	keys    []string
	ownedBy string
}{
	"claude":   {keys: []string{"claude"}, ownedBy: "anthropic"},
	"gemini":   {keys: []string{"gemini"}, ownedBy: "google"},
	"cx":       {keys: []string{"codex-free", "codex-team", "codex-plus", "codex-pro"}, ownedBy: "openai"},
	"ag":       {keys: []string{"antigravity"}, ownedBy: "google"},
	"kiro":     {keys: []string{"kimi"}, ownedBy: "moonshot"},
	"aistudio": {keys: []string{"aistudio"}, ownedBy: "google"},
}

// Models handles GET /v1/models — includes combos and virtual models.
func (h *Handler) Models(c *gin.Context) {
	prefixes := h.registry.List()
	var allModels []gin.H

	for _, prefix := range prefixes {
		for _, m := range h.getProviderModels(prefix) {
			allModels = append(allModels, m)
		}
	}

	// Add combo names as virtual models
	for _, combo := range h.combo.ListCombos() {
		allModels = append(allModels, gin.H{
			"id":       combo.Combo.Name,
			"object":   "model",
			"created":  combo.Combo.CreatedAt,
			"owned_by": "axonrouter",
		})
	}

	// Add virtual/smart models
	virtualModels := []string{"auto", "economy", "balanced", "premium"}
	for _, name := range virtualModels {
		allModels = append(allModels, gin.H{
			"id":       "smart/" + name,
			"object":   "model",
			"created":  1700000000,
			"owned_by": "axonrouter",
		})
	}

	if len(allModels) == 0 {
		allModels = h.defaultModels()
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   allModels,
	})
}

// getProviderModels returns models for a provider prefix from the auto-updating catalog.
// Returns nil for providers not in models.json (openai, groq, deepseek, etc.) —
// those are discovered dynamically from upstream and not served as static entries.
func (h *Handler) getProviderModels(prefix string) []gin.H {
	cfg, ok := v1ProviderCatalog[prefix]
	if !ok {
		return nil
	}

	ids := models.GetAllModelIDs(cfg.keys...)
	if len(ids) == 0 {
		return nil
	}

	entries := make([]gin.H, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, gin.H{
			"id":       prefix + "/" + id,
			"object":   "model",
			"created":  1700000000,
			"owned_by": cfg.ownedBy,
		})
	}
	return entries
}

// defaultModels returns all catalog-backed models as a fallback
// when no providers are registered (fresh install, no connections).
func (h *Handler) defaultModels() []gin.H {
	var all []gin.H
	for prefix := range v1ProviderCatalog {
		all = append(all, h.getProviderModels(prefix)...)
	}
	return all
}
