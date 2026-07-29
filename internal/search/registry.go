package search

import (
	"fmt"
	"os"
	"strings"
)

// ProviderCapability describes optional features a provider supports.
type ProviderCapability string

const (
	CapabilityWebSearch   ProviderCapability = "web_search"
	CapabilityImageSearch ProviderCapability = "image_search"
	CapabilityNewsSearch  ProviderCapability = "news_search"
	CapabilityAnswer      ProviderCapability = "answer"
	CapabilitySafeSearch  ProviderCapability = "safe_search"
	CapabilityTimeRange   ProviderCapability = "time_range"
	CapabilitySelfHosted  ProviderCapability = "self_hosted"
)

// ProviderConfig is the resolved configuration for a single search provider.
// It is registry-defined and can be overridden with per-request or
// per-deployment values.
type ProviderConfig struct {
	// Provider is the stable provider identifier.
	Provider Provider `json:"provider"`
	// DisplayName is a human-readable label.
	DisplayName string `json:"display_name"`
	// BaseURL is the upstream API origin. Empty means the provider has no
	// default and must be configured explicitly (e.g. searxng).
	BaseURL string `json:"base_url,omitempty"`
	// CredentialMode selects how the handler resolves the upstream credential.
	CredentialMode CredentialMode `json:"credential_mode"`
	// EnvVar is the environment variable read when CredentialMode is Env.
	EnvVar string `json:"env_var,omitempty"`
	// SettingsKey is the settings-table key read when CredentialMode is
	// SettingsTable.
	SettingsKey string `json:"settings_key,omitempty"`
	// DefaultParams are provider-specific parameters merged into every
	// request before the caller's Extra values.
	DefaultParams map[string]any `json:"default_params,omitempty"`
	// Capabilities is the set of features supported by this provider.
	Capabilities []ProviderCapability `json:"capabilities"`
}

// HasCapability reports whether the provider supports capability c.
func (pc ProviderConfig) HasCapability(c ProviderCapability) bool {
	for _, cap := range pc.Capabilities {
		if cap == c {
			return true
		}
	}
	return false
}

// Registry is a read-only catalog of search provider configurations.
// Create one with NewRegistry or use the default package-level registry.
type Registry struct {
	providers map[Provider]ProviderConfig
}

// NewRegistry creates a provider registry with sensible defaults.
// Override BaseURL or CredentialMode by passing functional options.
func NewRegistry(opts ...RegistryOption) *Registry {
	r := &Registry{providers: make(map[Provider]ProviderConfig)}
	for _, p := range knownProviderBuilders() {
		r.providers[p.Provider] = p
	}
	for _, opt := range opts {
		opt.apply(r)
	}
	return r
}

// Get returns the configuration for provider p. The second value is false when
// the provider is not registered.
func (r *Registry) Get(p Provider) (ProviderConfig, bool) {
	pc, ok := r.providers[p]
	return pc, ok
}

// GetByID is a convenience for looking up a provider by its string identifier.
func (r *Registry) GetByID(id string) (ProviderConfig, bool) {
	return r.Get(Provider(id))
}

// Providers returns all registered provider identifiers in stable order.
func (r *Registry) Providers() []Provider {
	out := make([]Provider, 0, len(r.providers))
	for _, p := range KnownProviders() {
		if _, ok := r.providers[p]; ok {
			out = append(out, p)
		}
	}
	return out
}

// Configs returns all registered provider configs in stable order.
func (r *Registry) Configs() []ProviderConfig {
	providers := r.Providers()
	out := make([]ProviderConfig, 0, len(providers))
	for _, p := range providers {
		out = append(out, r.providers[p])
	}
	return out
}

// DefaultProvider returns the configured default provider. It currently
// returns ProviderTavily.
func (r *Registry) DefaultProvider() Provider {
	return DefaultProvider
}

// RegistryOption customises a registry at construction time.
type RegistryOption interface {
	apply(*Registry)
}

type registryOptionFunc func(*Registry)

func (f registryOptionFunc) apply(r *Registry) { f(r) }

// WithBaseURL overrides the base URL for a provider.
func WithBaseURL(provider Provider, baseURL string) RegistryOption {
	return registryOptionFunc(func(r *Registry) {
		pc, ok := r.providers[provider]
		if !ok {
			return
		}
		pc.BaseURL = strings.TrimSuffix(baseURL, "/")
		r.providers[provider] = pc
	})
}

// WithCredentialMode overrides how credentials are resolved for a provider.
func WithCredentialMode(provider Provider, mode CredentialMode) RegistryOption {
	return registryOptionFunc(func(r *Registry) {
		pc, ok := r.providers[provider]
		if !ok {
			return
		}
		pc.CredentialMode = mode
		r.providers[provider] = pc
	})
}

// WithEnvVar sets the environment variable used when CredentialMode is Env.
func WithEnvVar(provider Provider, envVar string) RegistryOption {
	return registryOptionFunc(func(r *Registry) {
		pc, ok := r.providers[provider]
		if !ok {
			return
		}
		pc.EnvVar = envVar
		r.providers[provider] = pc
	})
}

// WithDefaultParam adds or replaces a default parameter for a provider.
func WithDefaultParam(provider Provider, key string, value any) RegistryOption {
	return registryOptionFunc(func(r *Registry) {
		pc, ok := r.providers[provider]
		if !ok {
			return
		}
		if pc.DefaultParams == nil {
			pc.DefaultParams = make(map[string]any)
		}
		pc.DefaultParams[key] = value
		r.providers[provider] = pc
	})
}

// ResolveCredential returns the upstream API key for provider p using the
// configured CredentialMode. It returns an empty string and no error when the
// mode is connection (callers must load credentials from the connection rows).
func ResolveCredential(pc ProviderConfig) (string, error) {
	switch pc.CredentialMode {
	case "", CredentialModeConnection:
		return "", nil
	case CredentialModeAPIKeyField:
		// The caller is responsible for populating this from the request body.
		return "", nil
	case CredentialModeEnv:
		if pc.EnvVar == "" {
			pc.EnvVar = defaultEnvVar(pc.Provider)
		}
		return os.Getenv(pc.EnvVar), nil
	case CredentialModeSettingsTable:
		// Settings-table reads are intentionally left to the handler / DB layer
		// so this package avoids importing internal/db.
		return "", nil
	default:
		return "", fmt.Errorf("unsupported credential_mode: %s", pc.CredentialMode)
	}
}

// defaultEnvVar returns the conventional environment variable name for a
// provider's API key.
func defaultEnvVar(p Provider) string {
	return "AXON_" + strings.ToUpper(string(p)) + "_API_KEY"
}

// knownProviderBuilders returns the Phase-1 provider definitions.
func knownProviderBuilders() []ProviderConfig {
	return []ProviderConfig{
		{
			Provider:       ProviderTavily,
			DisplayName:    "Tavily",
			BaseURL:        "https://api.tavily.com",
			CredentialMode: CredentialModeConnection,
			EnvVar:         defaultEnvVar(ProviderTavily),
			DefaultParams: map[string]any{
				"search_depth":   "basic",
				"include_answer": false,
			},
			Capabilities: []ProviderCapability{
				CapabilityWebSearch,
				CapabilityAnswer,
				CapabilitySafeSearch,
				CapabilityTimeRange,
			},
		},
		{
			Provider:       ProviderBrave,
			DisplayName:    "Brave Search",
			BaseURL:        "https://api.search.brave.com",
			CredentialMode: CredentialModeConnection,
			EnvVar:         defaultEnvVar(ProviderBrave),
			DefaultParams: map[string]any{
				"offset": 0,
			},
			Capabilities: []ProviderCapability{
				CapabilityWebSearch,
				CapabilityImageSearch,
				CapabilityNewsSearch,
				CapabilitySafeSearch,
			},
		},
		{
			Provider:       ProviderExa,
			DisplayName:    "Exa",
			BaseURL:        "https://api.exa.ai",
			CredentialMode: CredentialModeConnection,
			EnvVar:         defaultEnvVar(ProviderExa),
			DefaultParams: map[string]any{
				"use_autoprompt": true,
			},
			Capabilities: []ProviderCapability{
				CapabilityWebSearch,
				CapabilityAnswer,
				CapabilityTimeRange,
			},
		},
		{
			Provider:       ProviderSerper,
			DisplayName:    "Serper",
			BaseURL:        "https://google.serper.dev",
			CredentialMode: CredentialModeConnection,
			EnvVar:         defaultEnvVar(ProviderSerper),
			DefaultParams: map[string]any{
				"gl": "us",
				"hl": "en",
			},
			Capabilities: []ProviderCapability{
				CapabilityWebSearch,
				CapabilityImageSearch,
				CapabilityNewsSearch,
				CapabilitySafeSearch,
			},
		},
		{
			Provider:       ProviderGooglePSE,
			DisplayName:    "Google Custom Search",
			BaseURL:        "https://www.googleapis.com/customsearch/v1",
			CredentialMode: CredentialModeConnection,
			EnvVar:         defaultEnvVar(ProviderGooglePSE),
			Capabilities: []ProviderCapability{
				CapabilityWebSearch,
				CapabilityImageSearch,
			},
		},
		{
			Provider:       ProviderSearXNG,
			DisplayName:    "SearXNG",
			BaseURL:        "",
			CredentialMode: CredentialModeAPIKeyField,
			EnvVar:         defaultEnvVar(ProviderSearXNG),
			DefaultParams: map[string]any{
				"format": "json",
			},
			Capabilities: []ProviderCapability{
				CapabilityWebSearch,
				CapabilityImageSearch,
				CapabilityNewsSearch,
				CapabilitySelfHosted,
			},
		},
	}
}

// DefaultRegistry is the package-level registry used when no custom registry
// is supplied.
var DefaultRegistry = NewRegistry()
