package providercfg

import (
	"strings"
	"sync"
)

// Compatibility holds provider-specific request/response quirks that are too
// diverse to encode in the generic OpenAI executor. It is stored per-provider
// alongside ProviderSettings so operators can tune behavior without rebuilding.
type Compatibility struct {
	// ModelPrefix is prepended to bare model IDs before they are sent upstream.
	// Empty means no prefix is added. Example: "@cf/" for Cloudflare Workers AI.
	ModelPrefix string `json:"model_prefix,omitempty"`

	// MaxTokensCap is the maximum value allowed for max_tokens.
	MaxTokensCap int `json:"max_tokens_cap,omitempty"`

	// ReasoningMaxTokensCap is the cap applied to reasoning models.
	ReasoningMaxTokensCap int `json:"reasoning_max_tokens_cap,omitempty"`

	// FlattenContentArrays converts message content arrays to plain strings.
	FlattenContentArrays bool `json:"flatten_content_arrays,omitempty"`

	// StripProviderPrefix removes this literal prefix from model IDs.
	// Example: "bedrock/" for Bedrock Mantle.
	StripProviderPrefix string `json:"strip_provider_prefix,omitempty"`

	// ReasoningLevels lists accepted reasoning_effort values. Empty means the
	// field is passed through unchanged.
	ReasoningLevels []string `json:"reasoning_levels,omitempty"`

	// ExtraHeaders are added to every outgoing request for this provider.
	ExtraHeaders map[string]string `json:"extra_headers,omitempty"`
}

// zero-valued compatibility looks like "not configured"; for booleans that
// default to true we need pointer-like behaviour to distinguish "not set" from
// "explicitly false". For simplicity we keep the struct plain and seed defaults
// for known providers in defaultCompatibilities.

var (
	compatMu      sync.RWMutex
	compatManager *Manager
)

// defaultCompatibilities matches the historical hard-coded behaviour for the
// built-in providers. Custom providers start with a zero-value Compatibility
// unless the operator supplies one.
var defaultCompatibilities = map[string]Compatibility{
	"cf": {
		ModelPrefix:           "@cf/",
		MaxTokensCap:          8192,
		ReasoningMaxTokensCap: 4096,
		FlattenContentArrays:  true,
		ReasoningLevels:       []string{"none", "low", "medium", "high", "max"},
	},
	"bedrock": {
		StripProviderPrefix: "bedrock/",
	},
}

// setCompatibilityManager is called by NewManager so executors can read live
// per-provider overrides from the JSON settings files.
func setCompatibilityManager(m *Manager) {
	compatMu.Lock()
	defer compatMu.Unlock()
	compatManager = m
}

// CompatibilityFor returns the effective compatibility for a provider. It
// prefers the value stored by Manager, then the seeded default, then an empty
// Compatibility.
func CompatibilityFor(providerID string) Compatibility {
	compatMu.RLock()
	m := compatManager
	compatMu.RUnlock()

	if m != nil {
		s, err := m.Get(providerID)
		if err == nil && s.Compatibility != nil {
			return mergeCompatibilityDefaults(*s.Compatibility, defaultCompatibilities[providerID])
		}
	}

	if def, ok := defaultCompatibilities[providerID]; ok {
		return def
	}
	return Compatibility{}
}

// mergeCompatibilityDefaults fills zero-valued fields in src with values from
// def. This lets operators override only the quirks they care about while
// keeping the provider-specific defaults for the rest.
func mergeCompatibilityDefaults(src, def Compatibility) Compatibility {
	if src.ModelPrefix == "" {
		src.ModelPrefix = def.ModelPrefix
	}
	if src.MaxTokensCap == 0 {
		src.MaxTokensCap = def.MaxTokensCap
	}
	if src.ReasoningMaxTokensCap == 0 {
		src.ReasoningMaxTokensCap = def.ReasoningMaxTokensCap
	}
	if src.StripProviderPrefix == "" {
		src.StripProviderPrefix = def.StripProviderPrefix
	}
	if len(src.ReasoningLevels) == 0 {
		src.ReasoningLevels = def.ReasoningLevels
	}
	if len(src.ExtraHeaders) == 0 {
		src.ExtraHeaders = def.ExtraHeaders
	}
	// FlattenContentArrays intentionally keeps the stored value even when
	// false so operators can disable content flattening explicitly.
	return src
}

// HasReasoning reports whether a given reasoning_effort value is accepted by
// the provider.
func (c Compatibility) HasReasoning(level string) bool {
	if len(c.ReasoningLevels) == 0 {
		return true
	}
	level = strings.ToLower(strings.TrimSpace(level))
	for _, v := range c.ReasoningLevels {
		if v == level {
			return true
		}
	}
	return false
}
