package codexcatalog

import (
	"github.com/rickicode/AxonRouter-Go/internal/models"
)

// AvailableModel represents a single gateway model that can be turned into a
// Codex client catalog entry. It intentionally uses the same field names as
// models.json entries for drop-in compatibility.
type AvailableModel struct {
	ID            string
	DisplayName   string
	Description   string
	ContextLength int
}

// FlattenAvailableModels converts the gateway static catalog entries for the
// Codex provider family into the loosely-typed maps consumed by the builder.
// This keeps the existing models.json catalog as the single source of truth
// while giving the new builder real entries to work with.
func FlattenAvailableModels(providerKeys ...string) []map[string]any {
	ids := models.GetAllModelIDs(providerKeys...)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		displayName := ""
		for _, key := range providerKeys {
			names := models.GetModelDisplayNames(key)
			if names != nil && names[id] != "" {
				displayName = names[id]
				break
			}
		}
		out = append(out, map[string]any{
			"id":             id,
			"display_name":   displayName,
			"context_length": 0,
		})
	}
	return out
}

// BuildForProviderKeys returns a Codex client model response containing the
// union of models listed under the requested provider catalog keys. It is a
// convenience helper so handlers do not need to interact with the raw builder
// directly for the common static-catalog case.
func BuildForProviderKeys(keys ...string) map[string]any {
	available := FlattenAvailableModels(keys...)
	return NewBuilder(nil).BuildResponse(available)
}

// Handler is the public integration handle exposed to API handlers. It owns a
// Builder configured with the gateway's current provider registry and can be
// used to materialize a Codex client model catalog on demand.
type Handler struct {
	builder *Builder
}

// NewHandler returns a Handler that resolves providers via providersForModel.
func NewHandler(providersForModel ProvidersForModelFunc) *Handler {
	return &Handler{builder: NewBuilder(providersForModel)}
}

// Build returns a Codex client model response for the supplied available models.
func (h *Handler) Build(availableModels []map[string]any) map[string]any {
	return h.builder.BuildResponse(availableModels)
}

// BuildStatic returns a Codex client model response built from the current
// static models.json entries for the given provider catalog keys. This is the
// safe integration point for existing routes: it uses the static catalog and
// does not introduce any new runtime model discovery.
func (h *Handler) BuildStatic(providerKeys ...string) map[string]any {
	return h.Build(FlattenAvailableModels(providerKeys...))
}
