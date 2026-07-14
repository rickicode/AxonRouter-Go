package modalities

import (
	"encoding/json"
	"log"
	"sync"
)

type registryEntry struct {
	ProviderTypeID string              `json:"provider_type_id"`
	ServiceKinds   []string            `json:"service_kinds"`
	Models         map[string][]string `json:"models"`
}

var (
	loadOnce sync.Once
	registry map[string]registryEntry
)

func loadRegistry() {
	registry = make(map[string]registryEntry)

	files, err := registryFiles.ReadDir(".")
	if err != nil {
		log.Printf("WARN: failed to read modality registry files: %v", err)
		return
	}

	for _, f := range files {
		if f.IsDir() {
			continue
		}

		data, err := registryFiles.ReadFile(f.Name())
		if err != nil {
			log.Printf("WARN: failed to read modality registry file %s: %v", f.Name(), err)
			continue
		}

		var list []registryEntry
		if err := json.Unmarshal(data, &list); err != nil {
			log.Printf("WARN: failed to parse modality registry file %s: %v", f.Name(), err)
			continue
		}

		for _, e := range list {
			registry[e.ProviderTypeID] = e
		}
	}
}

func ensureLoaded() {
	loadOnce.Do(loadRegistry)
}

// Providers returns the provider type IDs present in the registry.
func Providers() []string {
	ensureLoaded()

	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}

// ServiceKinds returns the service kinds registered for a provider type.
func ServiceKinds(providerTypeID string) []string {
	ensureLoaded()

	e, ok := registry[providerTypeID]
	if !ok {
		return nil
	}

	out := make([]string, len(e.ServiceKinds))
	copy(out, e.ServiceKinds)
	return out
}

// Models returns the model IDs registered for a provider type and modality.
func Models(providerTypeID, modality string) []string {
	ensureLoaded()

	e, ok := registry[providerTypeID]
	if !ok {
		return nil
	}

	models, ok := e.Models[modality]
	if !ok {
		return nil
	}

	out := make([]string, len(models))
	copy(out, models)
	return out
}

// SupportsModel reports whether the model ID is registered for the given
// provider type and modality.
func SupportsModel(providerTypeID, modality, model string) bool {
	ensureLoaded()

	e, ok := registry[providerTypeID]
	if !ok {
		return false
	}

	for _, m := range e.Models[modality] {
		if m == model {
			return true
		}
	}
	return false
}
