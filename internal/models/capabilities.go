package models

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

//go:embed capabilities.json
var embeddedCapabilitiesJSON []byte

// ModelCapabilities describes input/output features a model supports.
type ModelCapabilities struct {
	Vision     bool `json:"vision"`
	PDF        bool `json:"pdf"`
	AudioInput bool `json:"audio_input"`
	VideoInput bool `json:"video_input"`
	Tools      bool `json:"tools"`
}

var (
	capsMu  sync.RWMutex
	capsMap map[string]ModelCapabilities
)

func init() {
	loadCapabilities()
}

func loadCapabilities() {
	capsMu.Lock()
	defer capsMu.Unlock()
	capsMap = make(map[string]ModelCapabilities)
	if len(embeddedCapabilitiesJSON) == 0 {
		return
	}
	var parsed map[string]ModelCapabilities
	if err := json.Unmarshal(embeddedCapabilitiesJSON, &parsed); err != nil {
		return
	}
	capsMap = parsed
}

// GetCapabilities returns capabilities for a given full model ID (e.g. openai/gpt-4o).
// It supports a basic wildcard fallback: if the exact ID is unknown, it tries
// progressively shorter suffixes separated by '-'.
// SupportsVision reports whether a model can consume image input. It combines
// the curated capability registry with provider aliases and the live catalog's
// image-to-text service kind, because runtime prefixes do not always match the
// vendor prefix used by capabilities.json.
func SupportsVision(modelID string) bool {
	clean := strings.TrimPrefix(strings.TrimSpace(modelID), "@")
	if clean == "" {
		return false
	}
	if GetCapabilities(clean).Vision {
		return true
	}
	provider, model := splitModelID(clean)
	for _, prefix := range providerAliases(provider) {
		if GetCapabilities(prefix+"/"+model).Vision || capabilityFamilyMatch(prefix, model) {
			return true
		}
		for _, candidate := range modelCandidates(prefix, model) {
			for _, kind := range GetModelServiceKinds(prefix, candidate) {
				if kind == "vision" || kind == "imageToText" {
					return true
				}
			}
		}
	}
	for _, candidate := range modelCandidates(provider, model) {
		for _, kind := range GetModelServiceKinds(provider, candidate) {
			if kind == "vision" || kind == "imageToText" {
				return true
			}
		}
	}
	return false
}

func capabilityFamilyMatch(provider, model string) bool {
	capsMu.RLock()
	defer capsMu.RUnlock()
	prefix := provider + "/"
	modelBase := trimDateSuffix(model)
	for id, caps := range capsMap {
		if !caps.Vision || !strings.HasPrefix(id, prefix) {
			continue
		}
		candidate := strings.TrimPrefix(id, prefix)
		candidateBase := trimDateSuffix(candidate)
		if candidateBase == modelBase || strings.HasPrefix(candidateBase, modelBase+"-") || strings.HasPrefix(modelBase, candidateBase+"-") {
			return true
		}
	}
	return false
}

// IsKnownModel reports whether the model is present in the curated capability
// registry or the current provider catalog. Unknown models are left untouched
// by the bridge to avoid destroying an image for a model that may support it.
func IsKnownModel(modelID string) bool {
	clean := strings.TrimPrefix(strings.TrimSpace(modelID), "@")
	if clean == "" {
		return false
	}
	if capabilityKnown(clean) {
		return true
	}
	provider, model := splitModelID(clean)
	for _, candidate := range modelCandidates(provider, model) {
		if HasModel(provider, candidate) {
			return true
		}
	}
	for _, alias := range providerAliases(provider) {
		for _, candidate := range modelCandidates(alias, model) {
			if HasModel(alias, candidate) || capabilityKnown(alias+"/"+strings.TrimPrefix(candidate, alias+"/")) {
				return true
			}
		}
	}
	return false
}

func capabilityKnown(modelID string) bool {
	capsMu.RLock()
	defer capsMu.RUnlock()
	if _, ok := capsMap[modelID]; ok {
		return true
	}
	provider, model := splitModelID(modelID)
	modelBase := trimDateSuffix(model)
	for id := range capsMap {
		if strings.HasPrefix(id, provider+"/") {
			candidate := strings.TrimPrefix(id, provider+"/")
			candidateBase := trimDateSuffix(candidate)
			if candidateBase == modelBase || strings.HasPrefix(candidateBase, modelBase+"-") || strings.HasPrefix(modelBase, candidateBase+"-") {
				return true
			}
		}
	}
	return false
}

func trimDateSuffix(model string) string {
	idx := strings.LastIndexByte(model, '-')
	if idx < 0 || len(model)-idx-1 != 8 {
		return model
	}
	for _, r := range model[idx+1:] {
		if r < '0' || r > '9' {
			return model
		}
	}
	return model[:idx]
}

func modelCandidates(provider, model string) []string {
	candidates := []string{model}
	if provider != "" && !strings.HasPrefix(model, provider+"/") {
		candidates = append(candidates, provider+"/"+model)
	}
	return candidates
}

func splitModelID(modelID string) (string, string) {
	parts := strings.SplitN(modelID, "/", 2)
	if len(parts) != 2 {
		return "", modelID
	}
	return parts[0], parts[1]
}

func providerAliases(provider string) []string {
	aliases := map[string]string{
		"claude":              "anthropic",
		"gemini":              "google",
		"ag":                  "google",
		"antigravity":         "google",
		"aistudio":            "google",
		"gemini-interactions": "google",
		"vertex":              "google",
		"cx":                  "openai",
	}
	if alias := aliases[provider]; alias != "" {
		return []string{alias}
	}
	return nil
}

func GetCapabilities(modelID string) ModelCapabilities {
	capsMu.RLock()
	defer capsMu.RUnlock()
	if c, ok := capsMap[modelID]; ok {
		return c
	}
	// Wildcard fallback for dated variants: openai/gpt-4o-2024-08-06 -> openai/gpt-4o
	for {
		idx := strings.LastIndex(modelID, "-")
		if idx <= 0 {
			break
		}
		modelID = modelID[:idx]
		if c, ok := capsMap[modelID]; ok {
			return c
		}
	}
	return ModelCapabilities{}
}
