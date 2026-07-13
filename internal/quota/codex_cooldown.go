package quota

import (
	"fmt"
	"time"
)

import "strings"

// CodexQuotaCooldown checks whether any quota window is exhausted and returns
// the cooldown deadline and reason. If no window reports a reset time, the
// cooldown defaults to 60 seconds from now.

// A window is considered exhausted when usage reaches 95% of its limit, matching
// OmniRoute dual-window behavior. This prevents over-blocking healthy accounts
// while still cooling down accounts that are near their limit.
func CodexQuotaCooldown(quotas []QuotaItem) (active bool, until time.Time, reason string) {
	now := time.Now()
	var earliestReset time.Time
	var exhausted []string
	for _, q := range quotas {
		// Unlimited or healthy windows do not trigger cooldown.
		if q.Unlimited || q.RemainingPct > 5 {
			continue
		}
		exhausted = append(exhausted, q.Name)
		reset := now.Add(60 * time.Second)
		if q.ResetAt != "" {
			// Prefer explicit reset time.
			if t, err := time.Parse(time.RFC3339, q.ResetAt); err == nil {
				reset = t
			}
		}
		if earliestReset.IsZero() || reset.Before(earliestReset) {
			earliestReset = reset
		}
	}
	if len(exhausted) == 0 {
		return false, time.Time{}, ""
	}
	return true, earliestReset, fmt.Sprintf("Codex quota near limit (>=95%%): %s", strings.Join(exhausted, ", "))
}
