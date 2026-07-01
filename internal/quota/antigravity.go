package quota

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var antigravityFetchURLs = []string{
	"https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels",
	"https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels",
	"https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:fetchAvailableModels",
}

var antigravityLoadURLs = []string{
	"https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:loadCodeAssist",
	"https://daily-cloudcode-pa.googleapis.com/v1internal:loadCodeAssist",
	"https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist",
}

const antigravityUserAgent = "Antigravity/1.0.0 (Linux) Chrome/132.0.6834.160 Electron/39.2.3"
const antigravityLoadUA = "vscode/1.X.X (Antigravity/1.0.0)"

// fetchAntigravityQuota fetches per-model quota from the Google Cloud Code API.
func fetchAntigravityQuota(accessToken string, psd map[string]any) ([]QuotaItem, string, error) {
	// 1. Get subscription info for plan label
	plan := fetchAntigravityPlan(accessToken)

	// 2. Get per-model quotas
	quotas, err := fetchAntigravityModels(accessToken, psd)
	if err != nil {
		return nil, plan, err
	}

	return quotas, plan, nil
}

func fetchAntigravityPlan(accessToken string) string {
	body := `{"metadata":{"ideType":"ANTIGRAVITY"}}`

	for _, url := range antigravityLoadURLs {
		req, err := http.NewRequest("POST", url, strings.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", antigravityLoadUA)

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}

		var data map[string]any
		if err := json.Unmarshal(respBody, &data); err != nil {
			continue
		}

		return mapCodeAssistToPlanLabel(data)
	}
	return ""
}

func fetchAntigravityModels(accessToken string, psd map[string]any) ([]QuotaItem, error) {
	// Build request body - include project if available
	reqBody := "{}"
	if pid, ok := psd["projectId"].(string); ok && pid != "" {
		reqBody = fmt.Sprintf(`{"project":"%s"}`, pid)
	}

	var lastErr error
	for _, url := range antigravityFetchURLs {
		req, err := http.NewRequest("POST", url, strings.NewReader(reqBody))
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", antigravityUserAgent)

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return nil, fmt.Errorf("antigravity token expired or access denied (HTTP %d)", resp.StatusCode)
		}
		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			lastErr = fmt.Errorf("antigravity api error %d: %s", resp.StatusCode, string(body))
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			continue
		}

		var data map[string]any
		if err := json.Unmarshal(body, &data); err != nil {
			lastErr = err
			continue
		}

		return parseAntigravityQuotas(data), nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("antigravity api unavailable")
}

// isNoiseModelSuffix returns true for model variants that are just resource tiers,
// not separate models worth tracking individually.
func isNoiseModelSuffix(modelKey string) bool {
	lower := strings.ToLower(modelKey)
	if strings.Contains(lower, "gpt-oss") {
		return true
	}
	suffixes := []string{"-low", "-high", "-agent", "-extra-low", "-lite", "-thinking", "-image"}
	for _, s := range suffixes {
		if strings.HasSuffix(lower, s) {
			return true
		}
	}
	return false
}

func parseAntigravityQuotas(data map[string]any) []QuotaItem {
	models, ok := data["models"].(map[string]any)
	if !ok {
		return nil
	}

	var quotas []QuotaItem
	for rawModelKey, infoVal := range models {
		info, ok := infoVal.(map[string]any)
		if !ok {
			continue
		}

		if isInternal, ok := info["isInternal"].(bool); ok && isInternal {
			continue
		}

		quotaInfo, ok := info["quotaInfo"].(map[string]any)
		if !ok || len(quotaInfo) == 0 {
			continue
		}

		modelKey := normalizeModelID(rawModelKey)
		if modelKey == "" {
			continue
		}

		// Skip noise variants (resource tiers, not real models)
		if isNoiseModelSuffix(modelKey) {
			continue
		}

		remainingFraction := getNumberField(quotaInfo, "remainingFraction", "remaining_fraction")
		resetAt := parseResetTimeString(quotaInfo)

		isUnlimited := remainingFraction >= 1.0 && resetAt == ""

		total := 1000.0
		remaining := total * remainingFraction
		used := total - remaining
		if isUnlimited {
			used = 0
			total = 0
		}

		family := classifyModelFamily(modelKey)

		quotas = append(quotas, QuotaItem{
			Name:         modelKey,
			Used:         used,
			Total:        total,
			RemainingPct: remainingFraction * 100,
			ResetAt:      resetAt,
			Unlimited:    isUnlimited,
			ModelKey:     modelKey,
			Family:       family,
		})
	}

	return quotas
}

func classifyModelFamily(modelKey string) string {
	lower := strings.ToLower(modelKey)
	if strings.Contains(lower, "gemini") {
		return "gemini"
	}
	if strings.Contains(lower, "claude") {
		return "claude"
	}
	return "other"
}

func normalizeModelID(raw string) string {
	// Trim whitespace, skip empty
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// Map common internal names
	lower := strings.ToLower(s)
	if strings.Contains(lower, "internal") || strings.Contains(lower, "test") {
		return ""
	}
	return s
}

func mapCodeAssistToPlanLabel(data map[string]any) string {
	// Try subscriptionInfo.cloudaicompanionProject structure
	sub, ok := data["subscriptionInfo"].(map[string]any)
	if !ok {
		// Try direct tier fields
		if tier, ok := data["tierId"].(string); ok {
			return mapTierToLabel(tier)
		}
		if tier, ok := data["subscriptionTier"].(string); ok {
			return mapTierStringToLabel(tier)
		}
		return ""
	}

	if tier, ok := sub["tierId"].(string); ok {
		if label := mapTierToLabel(tier); label != "" {
			return label
		}
	}

	if tier, ok := sub["subscriptionTier"].(string); ok {
		if label := mapTierStringToLabel(tier); label != "" {
			return label
		}
	}

	return ""
}

func mapTierToLabel(tierID string) string {
	lower := strings.ToLower(tierID)
	switch {
	case strings.Contains(lower, "free"):
		return "Free"
	case strings.Contains(lower, "pro") || strings.Contains(lower, "standard"):
		return "Pro"
	case strings.Contains(lower, "enterprise") || strings.Contains(lower, "business"):
		return "Enterprise"
	case strings.Contains(lower, "ultra") || strings.Contains(lower, "premium"):
		return "Ultra"
	}
	return ""
}

func mapTierStringToLabel(tier string) string {
	lower := strings.ToLower(tier)
	switch {
	case strings.Contains(lower, "free"):
		return "Free"
	case strings.Contains(lower, "pro"):
		return "Pro"
	case strings.Contains(lower, "enterprise"):
		return "Enterprise"
	case strings.Contains(lower, "ultra"):
		return "Ultra"
	}
	return ""
}

func parseResetTimeString(m map[string]any) string {
	if rt, ok := m["resetTime"].(string); ok && rt != "" {
		return rt
	}
	if rt, ok := m["reset_time"].(string); ok && rt != "" {
		return rt
	}
	// Try numeric (unix ms)
	if rn, ok := m["resetTime"].(float64); ok && rn > 0 {
		return time.UnixMilli(int64(rn)).UTC().Format(time.RFC3339)
	}
	if rn, ok := m["reset_time"].(float64); ok && rn > 0 {
		return time.UnixMilli(int64(rn)).UTC().Format(time.RFC3339)
	}
	return ""
}
