package combo

import "strings"

// SplitProviderModel splits a model identifier into provider prefix and model.
// It trims whitespace, strips a leading "@", and splits at the first "/".
// The provider and model must both be non-empty.
func SplitProviderModel(modelID string) (provider, model string, ok bool) {
	s := strings.TrimSpace(modelID)
	s = strings.TrimPrefix(s, "@")
	idx := strings.Index(s, "/")
	if idx <= 0 || idx+1 >= len(s) {
		return "", "", false
	}
	return s[:idx], s[idx+1:], true
}

// normalizeSmartGoal lowercases and trims a smart goal string.
func normalizeSmartGoal(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
