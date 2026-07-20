package connstate

import (
	"strings"
	"sync"

	provideralias "github.com/rickicode/AxonRouter-Go/internal/provider"
)

var perModelProviders = map[string]bool{
	"oc": true,
	"ag": true,
}

// antigravityScopeCache memoizes antigravityQuotaFamily results keyed by
// the original model string. Only Antigravity uses family logic today, so
// a per-model key is sufficient and avoids concatenation overhead.
var antigravityScopeCache sync.Map

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
	provider = provideralias.ResolveAlias(provider)
	if !perModelProviders[provider] {
		return model
	}
	switch provider {
	case "ag":
		if cached, ok := antigravityScopeCache.Load(model); ok {
			return cached.(string)
		}
		scope := antigravityQuotaFamily(model)
		antigravityScopeCache.Store(model, scope)
		return scope
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
