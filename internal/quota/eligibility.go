package quota

import (
	"strings"
)

// IsEligibleForModel returns false when a connection's quota state means it
// cannot serve a specific model. For Codex, this implements scope-aware
// routing: Spark quotas only block Spark models, and Codex quotas only block
// non-Spark models.
//
// A QuotaItem with RemainingPct <= 0 is treated as exhausted. Scope defaults
// to "codex" when empty.
func IsEligibleForModel(providerID, modelID string, quotas []QuotaItem) bool {
	if providerID != "cx" {
		return true
	}
	lowerModel := strings.ToLower(modelID)
	isSparkModel := strings.Contains(lowerModel, "spark")
	for _, q := range quotas {
		if q.RemainingPct > 0 {
			continue
		}
		scope := strings.ToLower(q.Scope)
		if scope == "" {
			scope = "codex"
		}
		if isSparkModel && scope == "spark" {
			return false
		}
		if !isSparkModel && scope == "codex" {
			return false
		}
	}
	return true
}
