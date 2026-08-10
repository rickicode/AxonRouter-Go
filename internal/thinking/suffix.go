package thinking

import "strings"

// ParseThinkingSuffix extracts a thinking-budget suffix from a model name.
//
// Supported suffix forms:
//   - model-name(N)        where N is an integer budget
//   - model-name(low)      discrete effort level
//   - model-name(medium)
//   - model-name(high)
//   - model-name(xhigh)
//   - model-name(max)
//   - model-name(minimal)
//   - model-name(none)     disables thinking
//   - model-name(auto)     enables dynamic thinking
//
// The suffix is case-insensitive. A valid suffix is only recognized when the
// model name ends with "(value)"; nested or unmatched parentheses are ignored.
//
// Returns the model name with the suffix stripped, the raw suffix value, and a
// flag indicating whether a suffix was found.
func ParseThinkingSuffix(model string) (base string, budgetOrLevel string, hasSuffix bool) {
	lastOpen := strings.LastIndex(model, "(")
	if lastOpen == -1 {
		return model, "", false
	}
	if !strings.HasSuffix(model, ")") {
		return model, "", false
	}

	raw := model[lastOpen+1 : len(model)-1]
	if raw == "" {
		return model, "", false
	}

	base = model[:lastOpen]
	return base, raw, true
}
