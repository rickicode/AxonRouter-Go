package quota

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const codexUsageURL = "https://chatgpt.com/backend-api/wham/usage"

// fetchCodexQuota fetches quota from the ChatGPT backend API.
// Returns session/weekly percentage quotas matching the OmniRoute buildCodexUsageQuotas pattern.
func fetchCodexQuota(accessToken string, psd map[string]any) ([]QuotaItem, string, error) {
	req, err := http.NewRequest("GET", codexUsageURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if wid, ok := psd["workspaceId"].(string); ok && wid != "" {
		req.Header.Set("chatgpt-account-id", wid)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("codex api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, "", fmt.Errorf("codex token expired or access denied (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("codex api error %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read response: %w", err)
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, "", fmt.Errorf("parse response: %w", err)
	}

	plan := getStringField(data, "plan_type", "planType")
	if plan == "" {
		plan = "unknown"
	}

	rateLimit := getMapField(data, "rate_limit", "rateLimit")
	quotas := parseCodexQuotas(rateLimit, data)

	return quotas, plan, nil
}

// parseCodexQuotas builds QuotaItems from the rate_limit block.
func parseCodexQuotas(rateLimit map[string]any, data map[string]any) []QuotaItem {
	var quotas []QuotaItem

	// Primary window (session)
	if pw := getMapField(rateLimit, "primary_window", "primaryWindow"); len(pw) > 0 {
		qi := buildPercentageQuota(pw, "Session")
		quotas = append(quotas, qi)
	}

	// Secondary window (weekly)
	if sw := getMapField(rateLimit, "secondary_window", "secondaryWindow"); len(sw) > 0 {
		qi := buildPercentageQuota(sw, "Weekly")
		quotas = append(quotas, qi)
	}

	// Code review rate limit (dedicated block or from additional_rate_limits)
	reviewRL := getMapField(data, "code_review_rate_limit", "codeReviewRateLimit")
	if len(reviewRL) == 0 {
		reviewRL = findAdditionalRateLimit(data, "code_review", "codex_review", "review")
	}
	if len(reviewRL) > 0 {
		if pw := getMapField(reviewRL, "primary_window", "primaryWindow"); len(pw) > 0 {
			quotas = append(quotas, buildPercentageQuota(pw, "Code Review"))
		}
		if sw := getMapField(reviewRL, "secondary_window", "secondaryWindow"); len(sw) > 0 {
			quotas = append(quotas, buildPercentageQuota(sw, "Code Review Weekly"))
		}
	}

	return quotas
}

func buildPercentageQuota(window map[string]any, name string) QuotaItem {
	usedPct := getNumberField(window, "used_percent", "usedPercent")
	resetAt := parseWindowReset(window)
	return QuotaItem{
		Name:         name,
		Used:         usedPct,
		Total:        100,
		RemainingPct: 100 - usedPct,
		ResetAt:      resetAt,
		Unlimited:    false,
	}
}

func parseWindowReset(window map[string]any) string {
	resetAt := getNumberField(window, "reset_at", "resetAt")
	if resetAt > 0 {
		return time.UnixMilli(int64(resetAt) * 1000).UTC().Format(time.RFC3339)
	}
	resetAfter := getNumberField(window, "reset_after_seconds", "resetAfterSeconds")
	if resetAfter > 0 {
		return time.Now().Add(time.Duration(resetAfter) * time.Second).UTC().Format(time.RFC3339)
	}
	return ""
}

// findAdditionalRateLimit scans additional_rate_limits for entries matching the given names.
func findAdditionalRateLimit(data map[string]any, names ...string) map[string]any {
	arl, ok := data["additional_rate_limits"]
	if !ok {
		arl, ok = data["additionalRateLimits"]
	}
	if !ok {
		return nil
	}

	list, ok := arl.([]any)
	if !ok {
		return nil
	}

	for _, entry := range list {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		for _, name := range names {
			if matchesFieldName(m, name) {
				rl := getMapField(m, "rate_limit", "rateLimit")
				if len(rl) > 0 {
					return rl
				}
			}
		}
	}
	return nil
}

func matchesFieldName(m map[string]any, target string) bool {
	keys := []string{"limit_name", "limitName", "metered_feature", "meteredFeature", "limit_id", "limitId", "id", "name", "title", "model", "model_id", "modelId"}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && containsIgnoreCase(s, target) {
				return true
			}
		}
	}
	return false
}

func containsIgnoreCase(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			sc := s[i+j]
			tc := substr[j]
			if sc >= 'A' && sc <= 'Z' {
				sc += 32
			}
			if tc >= 'A' && tc <= 'Z' {
				tc += 32
			}
			if sc != tc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
