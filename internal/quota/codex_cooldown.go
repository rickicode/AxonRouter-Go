package quota

import (
	"fmt"
	"time"
)

// CodexQuotaCooldown checks whether any quota window is exhausted and returns
// the cooldown deadline and reason. If no window reports a reset time, the
// cooldown defaults to 60 seconds from now.
func CodexQuotaCooldown(quotas []QuotaItem) (active bool, until time.Time, reason string) {
	now := time.Now()
	var earliestReset time.Time
	var exhausted []string
	for _, q := range quotas {
		if q.RemainingPct > 0 {
			continue
		}
		exhausted = append(exhausted, q.Name)
		reset := now.Add(60 * time.Second)
		if q.ResetAt != "" {
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
	return true, earliestReset, fmt.Sprintf("Codex quota exhausted: %s", joinNames(exhausted))
}

func joinNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	if len(names) == 1 {
		return names[0]
	}
	out := ""
	for i, n := range names[:len(names)-1] {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out + " and " + names[len(names)-1]
}
