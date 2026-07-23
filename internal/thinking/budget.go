package thinking

import (
	"strconv"
	"strings"
	"sync"
)

// Budget sentinels used after normalizing a suffix value.
const (
	// BudgetDisabled means thinking should be turned off.
	BudgetDisabled = 0
	// BudgetAuto means the provider should use its default/dynamic thinking mode.
	BudgetAuto = -1
)

// Known thinking-effort levels and their representative token budgets.
var levelBudgets = map[string]int{
	"none":    BudgetDisabled,
	"auto":    BudgetAuto,
	"minimal": 512,
	"low":     1024,
	"medium":  8192,
	"high":    24576,
	"xhigh":   32768,
	"max":     128000,
}

// providerBudgetClamps define sane per-provider default ranges for budget-style
// thinking. Custom providers and unknown providers fall back to genericClamp.
var providerBudgetClamps = map[string]*Clamp{
	"claude":      {Min: 1024, Max: 128000},
	"gemini":      {Min: 1, Max: 24576},
	"ag":          {Min: 1, Max: 128000},
	"antigravity": {Min: 1, Max: 128000},
}

var genericClamp = Clamp{Min: 1, Max: 128000}
var clampMu sync.RWMutex

// Clamp holds an inclusive min/max budget range.
type Clamp struct {
	Min int
	Max int
}

// BudgetFromString maps a raw suffix value to a normalized integer budget.
// Accepted inputs:
//   - known level names ("low", "medium", "high", "xhigh", "max", "minimal")
//   - "none" or the integer "-1" both disable thinking (=> 0)
//   - "auto" enables dynamic thinking (=> BudgetAuto)
//   - any non-negative decimal integer
func BudgetFromString(raw string) (int, bool) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if v, ok := levelBudgets[raw]; ok {
		return v, true
	}
	if raw == "-1" {
		return BudgetDisabled, true
	}

	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	if v < 0 {
		return 0, false
	}
	return v, true
}

// ClampBudget enforces the default per-provider budget range.
func ClampBudget(provider string, budget int) int {
	if budget <= BudgetDisabled {
		return budget
	}

	clampMu.RLock()
	clamp := providerBudgetClamps[strings.ToLower(strings.TrimSpace(provider))]
	clampMu.RUnlock()
	if clamp == nil {
		clamp = &genericClamp
	}

	if budget < clamp.Min {
		return clamp.Min
	}
	if budget > clamp.Max {
		return clamp.Max
	}
	return budget
}
