// Package codexcatalog builds dynamic Codex client model catalogs from
// gateway-side model definitions while leaving the static models.json catalog
// unchanged.
package codexcatalog

//go:generate go run ./gen.go

import (
	_ "embed"
)

//go:embed codex_client_models.json
var embeddedCodexClientModelsJSON []byte

// loadBuiltInTemplates returns the static embedded Codex client model templates.
func loadBuiltInTemplates() map[string]map[string]any {
	templates, _, _ := loadTemplatesFromJSON(embeddedCodexClientModelsJSON)
	if templates == nil {
		return map[string]map[string]any{}
	}
	return templates
}

// loadBuiltInDefaultTemplate returns the default template from the embedded
// static catalog. gpt-5.5 is used as the canonical default.
func loadBuiltInDefaultTemplate() map[string]any {
	_, defaultTemplate, _ := loadTemplatesFromJSON(embeddedCodexClientModelsJSON)
	return defaultTemplate
}
