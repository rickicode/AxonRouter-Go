package v1

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rickicode/AxonRouter-Go/internal/models"
)

// v1ProviderCatalog maps provider prefix → models.json keys + owned_by.
var v1ProviderCatalog = map[string]struct {
	keys    []string
	ownedBy string
}{
	"claude":        {keys: []string{"claude"}, ownedBy: "anthropic"},
	"gemini":        {keys: []string{"gemini"}, ownedBy: "google"},
	"cx":            {keys: []string{"codex-free", "codex-team", "codex-plus", "codex-pro"}, ownedBy: "openai"},
	"ag":            {keys: []string{"antigravity"}, ownedBy: "google"},
	"kiro":          {keys: []string{"kiro"}, ownedBy: "amazon"},
	"aistudio":      {keys: []string{"aistudio"}, ownedBy: "google"},
	"oc":            {keys: []string{"oc"}, ownedBy: "opencode"},
	"oc-zen":        {keys: []string{"oc-zen"}, ownedBy: "opencode"},
	"oc-go":         {keys: []string{"oc-go"}, ownedBy: "opencode"},
	"mimocode":      {keys: []string{"mimocode"}, ownedBy: "xiaomi"},
	"mimocode-free": {keys: []string{"mimocode"}, ownedBy: "xiaomi"},
	"mimo":          {keys: []string{"mimocode"}, ownedBy: "xiaomi"},
	"mimo-tp":       {keys: []string{"mimocode"}, ownedBy: "xiaomi"},
	"mimo-token":    {keys: []string{"mimocode"}, ownedBy: "xiaomi"},
	"openai":        {keys: []string{"openai"}, ownedBy: "openai"},
	"groq":          {keys: []string{"groq"}, ownedBy: "groq"},
	"deepseek":      {keys: []string{"deepseek"}, ownedBy: "deepseek"},
	"openrouter": {keys: []string{"openrouter"}, ownedBy: "openrouter"},
	"vertex": {keys: []string{"vertex"}, ownedBy: "google"},
	"zai": {keys: []string{"claude"}, ownedBy: "zai"},
	"cf":            {keys: []string{"cf"}, ownedBy: "cloudflare"},
}

// buildModelList returns the unified gateway model catalog: registered providers,
// combo names, and smart virtual models.
func (h *Handler) buildModelList() []gin.H {
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

	return allModels
}

// ListModels exposes the internal list of resolved model entries.
// Other handlers (e.g. CLI Tools) reuse it so the model catalog stays single-source.
func (h *Handler) ListModels() []gin.H {
	return h.buildModelList()
}

// ListActiveModels returns only models from providers that have at least one
// connection added, plus combos and smart virtual models. Used by the dashboard
// model picker so users only browse models they can actually route to.
func (h *Handler) ListActiveModels() []gin.H {
	// Only providers that have at least one enabled (active) connection are
	// considered routable, so /v1/models reflects providers the gateway can
	// actually serve. Combos and smart virtual models are always included.
	rows, err := h.db.Query(`SELECT DISTINCT provider_type_id FROM connections WHERE is_active = 1`)
	if err != nil {
		return h.buildModelList()
	}
	defer rows.Close()
	connected := make(map[string]bool)
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil {
			connected[p] = true
		}
	}
	if err := rows.Err(); err != nil {
		return h.buildModelList()
	}

	all := h.buildModelList()
	var result []gin.H
	for _, m := range all {
		id, _ := m["id"].(string)
		ownedBy, _ := m["owned_by"].(string)
		// Combos and smart virtual models are always available
		if ownedBy == "axonrouter" {
			result = append(result, m)
			continue
		}
		prefix := strings.SplitN(id, "/", 2)[0]
		if connected[prefix] {
			result = append(result, m)
		}
	}

	return result
}

// Models handles GET /v1/models — includes combos and virtual models.
func (h *Handler) Models(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   h.ListActiveModels(),
	})
}

// getProviderModels returns models for a provider prefix from the auto-updating catalog.
// Returns nil for providers not in models.json (openai, groq, deepseek, etc.) —
// those are discovered dynamically from upstream and not served as static entries.
func (h *Handler) getProviderModels(prefix string) []gin.H {
	cfg, ok := v1ProviderCatalog[prefix]
	if !ok {
		// Not a catalog-backed provider: serve user-added custom models.
		return h.customModels(prefix)
	}
	// For Cloudflare, enrich the shared catalog from the official Workers AI model
	// search endpoint using a live ready connection. This is idempotent and cached
	// in-memory so subsequent /v1/models calls reflect the same list.
	if prefix == "cf" {
		h.discoverCloudflareModels()
	}
	ids := models.GetAllModelIDs(cfg.keys...)
	if len(ids) == 0 {
		return nil
	}
	entries := make([]gin.H, 0, len(ids))
	for _, id := range ids {
		// Strip leading "@" — CF models use "@cf/vendor/model" format
		cleanID := strings.TrimPrefix(id, "@")
		// Avoid double prefix: if catalog ID already starts with prefix/, don't prepend again
		modelID := cleanID
		if !strings.HasPrefix(cleanID, prefix+"/") {
			modelID = prefix + "/" + cleanID
		}
	var serviceKinds []string
	for _, key := range cfg.keys {
		if kinds := models.GetModelServiceKinds(key, id); len(kinds) > 0 {
			serviceKinds = kinds
			break
		}
	}
	// Non-CF providers in this catalog are OpenAI-compatible text providers, so
	// default any model without explicit tags to LLM.
	if len(serviceKinds) == 0 && prefix != "cf" {
		serviceKinds = []string{"llm"}
	}
	entry := gin.H{
		"id": modelID,
		"object": "model",
		"created": 1700000000,
		"owned_by": cfg.ownedBy,
	}
	if len(serviceKinds) > 0 {
		entry["service_kinds"] = serviceKinds
	}
		entries = append(entries, entry)
	}
	return entries
}

// discoverCloudflareModels fetches the live Workers AI model list from a ready CF
// connection and merges it (with service kinds) into the shared catalog. Results
// are cached for cfDiscoveryTTL so /v1/models does not hit Cloudflare on every
// request.
func (h *Handler) discoverCloudflareModels() {
	var apiKey, psdJSON string
	err := h.db.QueryRow(`SELECT COALESCE(api_key,''), COALESCE(provider_specific_data,'') FROM connections WHERE provider_type_id = 'cf' AND status IN ('ready','degraded') AND is_active = 1 LIMIT 1`).Scan(&apiKey, &psdJSON)
	if err != nil || apiKey == "" {
		return
	}
	psd := make(map[string]string)
	if psdJSON != "" {
		_ = json.Unmarshal([]byte(psdJSON), &psd)
	}
	accountID := psd["accountId"]
	if accountID == "" {
		return
	}
	models.DiscoverCloudflareModelsCached(apiKey, accountID)
}

// customModels returns user-added models stored for a provider prefix (custom providers).
func (h *Handler) customModels(prefix string) []gin.H {
	rows, err := h.db.Query(`SELECT model FROM provider_models WHERE provider_type_id = ? ORDER BY model`, prefix)
	if err != nil {
		return nil
	}
	defer rows.Close()
	entries := make([]gin.H, 0)
	for rows.Next() {
		var model string
		if rows.Scan(&model) != nil || model == "" {
			continue
		}
		entries = append(entries, gin.H{
			"id":       prefix + "/" + model,
			"object":   "model",
			"created":  1700000000,
			"owned_by": "custom",
		})
	}
	if err := rows.Err(); err != nil {
		return nil
	}
	if len(entries) == 0 {
		return nil
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
