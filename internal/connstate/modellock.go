package connstate

import (
	"strings"

	provideralias "github.com/rickicode/AxonRouter-Go/internal/provider"
)

var perModelProviders = map[string]bool{
	"oc": true,
	"ag": true,
}

// HasPerModelQuota returns true if the provider has independent per-model quotas
// on a single connection. For these providers a 429/quota error on one model must
// not block other models on the same connection.
func HasPerModelQuota(provider string) bool {
	provider = provideralias.ResolveAlias(provider)
	return perModelProviders[provider]
}

// ModelScope returns the exhaustion/cooldown scope for a given provider and model.
// For per-model providers the scope is the model itself; for Antigravity it is the
// quota family (family:gemini or family:claude). Returns the original model for
// non-per-model providers (the value is unused by callers in that case).
func ModelScope(provider, model string) string {
	if !HasPerModelQuota(provider) {
		return model
	}
	provider = provideralias.ResolveAlias(provider)
	switch provider {
	case "ag":
		return antigravityQuotaFamily(model)
	default:
		return model
	}
}

// antigravityQuotaFamily maps an Antigravity model to its shared quota family.
// gemini-* models share one family; claude-*/cloud-*/anthropic-* share another.
// Everything else keeps exact-model scope.
func antigravityQuotaFamily(m string) string {
	b := strings.ToLower(strings.TrimPrefix(m, "ag/"))
	switch {
	case strings.Contains(b, "gemini"):
		return "family:gemini"
	case strings.HasPrefix(b, "claude-"), strings.HasPrefix(b, "cloud-"), strings.Contains(b, "anthropic"):
		return "family:claude"
	default:
		return m
	}
}
