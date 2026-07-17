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
