package search

import (
	"os"
	"testing"
)

func TestNewRegistryContainsAllPhase1Providers(t *testing.T) {
	r := NewRegistry()
	providers := r.Providers()
	if len(providers) != len(KnownProviders()) {
		t.Fatalf("registry has %d providers, want %d", len(providers), len(KnownProviders()))
	}
	for _, p := range KnownProviders() {
		if _, ok := r.Get(p); !ok {
			t.Errorf("provider %s not found in registry", p)
		}
	}
}

func TestRegistryGetByID(t *testing.T) {
	r := NewRegistry()
	pc, ok := r.GetByID("tavily")
	if !ok {
		t.Fatal("expected tavily config")
	}
	if pc.Provider != ProviderTavily {
		t.Errorf("provider = %s, want tavily", pc.Provider)
	}
}

func TestRegistryGetUnknown(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.Get(Provider("bing")); ok {
		t.Error("unknown provider should not be in registry")
	}
}

func TestRegistryDefaultProvider(t *testing.T) {
	r := NewRegistry()
	if r.DefaultProvider() != ProviderTavily {
		t.Errorf("default = %s, want tavily", r.DefaultProvider())
	}
}

func TestProviderConfigCapabilities(t *testing.T) {
	r := NewRegistry()
	tavily, ok := r.Get(ProviderTavily)
	if !ok {
		t.Fatal("tavily missing")
	}
	if !tavily.HasCapability(CapabilityWebSearch) {
		t.Error("tavily should support web_search")
	}
	if !tavily.HasCapability(CapabilityAnswer) {
		t.Error("tavily should support answer")
	}
	if tavily.HasCapability(CapabilitySelfHosted) {
		t.Error("tavily should not be self_hosted")
	}

	searxng, ok := r.Get(ProviderSearXNG)
	if !ok {
		t.Fatal("searxng missing")
	}
	if !searxng.HasCapability(CapabilitySelfHosted) {
		t.Error("searxng should be self_hosted")
	}
}

func TestRegistryWithBaseURL(t *testing.T) {
	r := NewRegistry(WithBaseURL(ProviderBrave, "https://brave.example.com/"))
	pc, ok := r.Get(ProviderBrave)
	if !ok {
		t.Fatal("brave missing")
	}
	if pc.BaseURL != "https://brave.example.com" {
		t.Errorf("base_url = %q, want no trailing slash", pc.BaseURL)
	}
}

func TestRegistryWithCredentialMode(t *testing.T) {
	r := NewRegistry(WithCredentialMode(ProviderSearXNG, CredentialModeEnv))
	pc, _ := r.Get(ProviderSearXNG)
	if pc.CredentialMode != CredentialModeEnv {
		t.Errorf("credential_mode = %s, want env", pc.CredentialMode)
	}
}

func TestRegistryWithDefaultParam(t *testing.T) {
	r := NewRegistry(WithDefaultParam(ProviderTavily, "search_depth", "advanced"))
	pc, _ := r.Get(ProviderTavily)
	if pc.DefaultParams["search_depth"] != "advanced" {
		t.Errorf("search_depth = %v, want advanced", pc.DefaultParams["search_depth"])
	}
}

func TestResolveCredentialEnv(t *testing.T) {
	envVar := "AXON_TEST_SEARCH_KEY"
	_ = os.Unsetenv(envVar)
	defer os.Unsetenv(envVar)

	pc := ProviderConfig{
		Provider:       ProviderTavily,
		CredentialMode: CredentialModeEnv,
		EnvVar:         envVar,
	}
	val, err := ResolveCredential(pc)
	if err != nil {
		t.Fatalf("ResolveCredential error: %v", err)
	}
	if val != "" {
		t.Errorf("empty env should give empty string, got %q", val)
	}

	_ = os.Setenv(envVar, "secret123")
	val, err = ResolveCredential(pc)
	if err != nil {
		t.Fatalf("ResolveCredential error: %v", err)
	}
	if val != "secret123" {
		t.Errorf("env value = %q, want secret123", val)
	}
}

func TestResolveCredentialConnection(t *testing.T) {
	pc := ProviderConfig{Provider: ProviderBrave, CredentialMode: CredentialModeConnection}
	val, err := ResolveCredential(pc)
	if err != nil {
		t.Fatalf("ResolveCredential error: %v", err)
	}
	if val != "" {
		t.Errorf("connection mode should return empty, got %q", val)
	}
}

func TestResolveCredentialUnsupported(t *testing.T) {
	pc := ProviderConfig{Provider: ProviderTavily, CredentialMode: CredentialMode("magic")}
	if _, err := ResolveCredential(pc); err == nil {
		t.Error("expected error for unsupported credential mode")
	}
}

func TestDefaultEnvVar(t *testing.T) {
	if defaultEnvVar(ProviderTavily) != "AXON_TAVILY_API_KEY" {
		t.Errorf("unexpected default env var: %s", defaultEnvVar(ProviderTavily))
	}
}

func TestDefaultRegistry(t *testing.T) {
	if DefaultRegistry == nil {
		t.Fatal("DefaultRegistry is nil")
	}
	if _, ok := DefaultRegistry.Get(ProviderExa); !ok {
		t.Error("DefaultRegistry missing exa")
	}
}
