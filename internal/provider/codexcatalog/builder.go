// Package codexcatalog builds dynamic Codex client model catalogs from
// gateway-side model definitions while leaving the static models.json catalog
// unchanged.
package codexcatalog

import (
	"encoding/json"
	"sort"
	"strings"
)

// ProvidersForModelFunc returns the providers registered for a model.
type ProvidersForModelFunc func(string) []string

// Builder constructs a Codex client model catalog from available models.
type Builder struct {
	providersForModel  ProvidersForModelFunc
	enableMultiAgentV2 bool
}

// NewBuilder returns a catalog builder with the given provider resolver.
func NewBuilder(providersForModel ProvidersForModelFunc) *Builder {
	return &Builder{providersForModel: providersForModel}
}

// WithMultiAgentV2 enables the synthetic multi_agent_version="v2" marker for
// newly-synthesized model entries.
func (b *Builder) WithMultiAgentV2(enable bool) *Builder {
	b.enableMultiAgentV2 = enable
	return b
}

// BuildResponse builds the top-level Codex client model response from a list of
// gateway available models. Each model is expected to contain at least an "id"
// field and may include display_name, description, and context_length.
func (b *Builder) BuildResponse(availableModels []map[string]any) map[string]any {
	return map[string]any{
		"models": b.buildModels(availableModels),
	}
}

func (b *Builder) buildModels(models []map[string]any) []map[string]any {
	templates := b.templates()
	defaultTemplate := b.defaultTemplate()
	if defaultTemplate == nil {
		return nil
	}

	result := make([]map[string]any, 0, len(models))
	for _, model := range models {
		id := strings.TrimSpace(stringValue(model, "id"))
		if id == "" {
			continue
		}

		var entry map[string]any
		if template, ok := templates[id]; ok {
			entry = cloneMap(template)
			applyDisplayName(entry, model)
		} else {
			entry = cloneMap(defaultTemplate)
			applySynthesizedMetadata(entry, id, model, b.enableMultiAgentV2)
		}

		applySearchToolSupport(entry, id, templates, b.providersForModel)
		sanitizeReasoningMetadata(entry)
		applyVisibilityOverride(entry, id)
		result = append(result, entry)
	}

	applyNonTemplatePriorities(result, templates)
	sort.SliceStable(result, func(i, j int) bool {
		return modelPriority(result[i]) < modelPriority(result[j])
	})
	return result
}

// templates returns the currently loaded template catalog. Out of scope of
// this issue (priority/template filtering), the method returns the static
// built-in templates so the builder is usable even when no external catalog has
// been loaded. Future work can swap this for a live template store without
// changing callers.
func (b *Builder) templates() map[string]map[string]any {
	globalCatalog.mu.RLock()
	defer globalCatalog.mu.RUnlock()
	if !globalCatalog.loaded {
		return loadBuiltInTemplates()
	}
	return globalCatalog.templates
}

// defaultTemplate returns the default fallback template used for models not
// present in the explicit template set.
func (b *Builder) defaultTemplate() map[string]any {
	globalCatalog.mu.RLock()
	defer globalCatalog.mu.RUnlock()
	if !globalCatalog.loaded {
		return loadBuiltInDefaultTemplate()
	}
	return globalCatalog.defaultTemplate
}

func loadTemplatesFromJSON(raw []byte) (map[string]map[string]any, map[string]any, error) {
	var payload struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil, err
	}
	templates := make(map[string]map[string]any, len(payload.Models))
	var defaultTemplate map[string]any
	for _, model := range payload.Models {
		slug := strings.TrimSpace(stringValue(model, "slug"))
		if slug == "" {
			continue
		}
		templates[slug] = cloneMap(model)
		if slug == "gpt-5.5" {
			defaultTemplate = cloneMap(model)
		}
	}
	return templates, defaultTemplate, nil
}

// SetTemplates loads an external Codex client model template catalog. It is
// safe for concurrent use callers may refresh the catalog at runtime.
func SetTemplates(raw []byte) error {
	templates, defaultTemplate, err := loadTemplatesFromJSON(raw)
	if err != nil {
		return err
	}
	globalCatalog.mu.Lock()
	defer globalCatalog.mu.Unlock()
	globalCatalog.templates = templates
	globalCatalog.defaultTemplate = defaultTemplate
	globalCatalog.loaded = true
	return nil
}

// ClearTemplates resets the builder to use the built-in static template catalog.
func ClearTemplates() {
	globalCatalog.mu.Lock()
	defer globalCatalog.mu.Unlock()
	globalCatalog.templates = nil
	globalCatalog.defaultTemplate = nil
	globalCatalog.loaded = false
}

func applyDisplayName(entry, model map[string]any) {
	if displayName := stringValue(model, "display_name"); displayName != "" {
		entry["display_name"] = displayName
	}
}

func applySynthesizedMetadata(entry map[string]any, id string, model map[string]any, optimizeMultiAgentV2 bool) {
	displayName := stringValue(model, "display_name")
	description := stringValue(model, "description")
	contextWindow := intValue(model, "context_length")

	if displayName == "" {
		displayName = id
	}
	if description == "" {
		description = id
	}

	entry["slug"] = id
	entry["display_name"] = displayName
	entry["description"] = description
	entry["prefer_websockets"] = false
	if optimizeMultiAgentV2 {
		entry["multi_agent_version"] = "v2"
	}
	entry["service_tiers"] = []any{}
	delete(entry, "apply_patch_tool_type")
	delete(entry, "upgrade")
	delete(entry, "availability_nux")

	if contextWindow > 0 {
		entry["context_window"] = contextWindow
		entry["max_context_window"] = contextWindow
	}
	if baseInstructions := stringValue(model, "base_instructions"); baseInstructions != "" {
		entry["base_instructions"] = baseInstructions
	}
	if plans, ok := model["available_in_plans"]; ok {
		entry["available_in_plans"] = cloneValue(plans)
	}
}

func applySearchToolSupport(entry map[string]any, id string, templates map[string]map[string]any, providersForModel ProvidersForModelFunc) {
	supportsSearch, _ := entry["supports_search_tool"].(bool)
	if !supportsSearch {
		return
	}
	if _, templateExists := templates[id]; !templateExists {
		entry["supports_search_tool"] = false
		return
	}
	if providersForModel == nil {
		return
	}
	providers := providersForModel(id)
	if len(providers) == 0 {
		entry["supports_search_tool"] = false
		return
	}
	for _, provider := range providers {
		if !strings.EqualFold(strings.TrimSpace(provider), "codex") {
			entry["supports_search_tool"] = false
			return
		}
	}
}

func applyVisibilityOverride(entry map[string]any, id string) {
	switch strings.TrimSpace(id) {
	case "grok-imagine-image-quality", "gpt-image-1.5", "gpt-image-2",
		"grok-imagine-image", "grok-imagine-video", "grok-imagine-video-1.5-preview":
		entry["visibility"] = "hide"
	}
}

func applyNonTemplatePriorities(result []map[string]any, templates map[string]map[string]any) {
	if len(result) == 0 {
		return
	}
	basePriority := maxTemplatePriority(templates)
	type pending struct {
		index       int
		displayName string
		slug        string
	}
	var nonTemplates []pending
	for index, entry := range result {
		slug := stringValue(entry, "slug")
		if _, ok := templates[slug]; ok {
			continue
		}
		displayName := stringValue(entry, "display_name")
		if displayName == "" {
			displayName = slug
		}
		nonTemplates = append(nonTemplates, pending{index: index, displayName: displayName, slug: slug})
	}

	sort.SliceStable(nonTemplates, func(i, j int) bool {
		left := strings.ToLower(nonTemplates[i].displayName)
		right := strings.ToLower(nonTemplates[j].displayName)
		if left == right {
			return nonTemplates[i].slug < nonTemplates[j].slug
		}
		return left < right
	})

	for rank, p := range nonTemplates {
		result[p.index]["priority"] = basePriority + 100*(rank+1)
	}
}

func maxTemplatePriority(templates map[string]map[string]any) int {
	maxPriority := 0
	for _, template := range templates {
		priority := modelPriority(template)
		if priority > maxPriority {
			maxPriority = priority
		}
	}
	return maxPriority
}

func sanitizeReasoningMetadata(entry map[string]any) {
	rawLevels, ok := entry["supported_reasoning_levels"].([]any)
	if !ok {
		return
	}
	levels := make([]any, 0, len(rawLevels))
	allowedDefaults := make(map[string]struct{}, len(rawLevels))
	for _, rawLevelEntry := range rawLevels {
		levelEntry, ok := rawLevelEntry.(map[string]any)
		if !ok {
			continue
		}
		level := normalizeReasoningLevel(stringValue(levelEntry, "effort"))
		if level == "" {
			continue
		}
		cloned := cloneMap(levelEntry)
		cloned["effort"] = level
		levels = append(levels, cloned)
		allowedDefaults[level] = struct{}{}
	}
	if len(levels) == 0 {
		delete(entry, "supported_reasoning_levels")
		delete(entry, "default_reasoning_level")
		return
	}
	defaultLevel := normalizeReasoningLevel(stringValue(entry, "default_reasoning_level"))
	if _, ok := allowedDefaults[defaultLevel]; !ok {
		defaultLevel = stringValue(levels[0].(map[string]any), "effort")
	}
	entry["supported_reasoning_levels"] = levels
	entry["default_reasoning_level"] = defaultLevel
}

func normalizeReasoningLevel(raw string) string {
	level := strings.ToLower(strings.TrimSpace(raw))
	if _, ok := allowedReasoningLevels[level]; !ok {
		return ""
	}
	return level
}

func reasoningDescription(level string) string {
	switch level {
	case "none":
		return "No reasoning"
	case "minimal":
		return "Fastest responses with minimal reasoning"
	case "low":
		return "Fast responses with lighter reasoning"
	case "medium":
		return "Balances speed and reasoning depth for everyday tasks"
	case "high":
		return "Greater reasoning depth for complex problems"
	case "xhigh":
		return "Extra high reasoning depth for complex problems"
	case "max":
		return "Maximum available reasoning depth for complex problems"
	default:
		return level
	}
}

func modelPriority(model map[string]any) int {
	if priority, ok := model["priority"].(int); ok {
		return priority
	}
	if priority, ok := model["priority"].(float64); ok {
		return int(priority)
	}
	return 100
}
