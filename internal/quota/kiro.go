package quota

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const kiroDefaultBaseURL = "https://codewhisperer.us-east-1.amazonaws.com"

// fetchKiroQuota fetches quota from AWS CodeWhisperer (Kiro) API.
func fetchKiroQuota(accessToken string, psd map[string]any) ([]QuotaItem, string, error) {
	profileArn := ""
	if v, ok := psd["profileArn"].(string); ok {
		profileArn = v
	}

	region := "us-east-1"
	if v, ok := psd["region"].(string); ok && v != "" {
		region = strings.ToLower(v)
	}

	// Derive region from profileArn if not explicitly stored
	if region == "us-east-1" && profileArn != "" {
		if parts := strings.Split(profileArn, ":"); len(parts) >= 4 {
			if r := parts[3]; r != "" {
				region = r
			}
		}
	}

	usageBaseURL := kiroDefaultBaseURL
	if region != "us-east-1" {
		usageBaseURL = fmt.Sprintf("https://q.%s.amazonaws.com", region)
	}

	// Discover profileArn if missing
	if profileArn == "" {
		profileArn = discoverKiroProfileArn(accessToken, usageBaseURL)
	}

	if profileArn == "" {
		return nil, "", fmt.Errorf("kiro profile ARN not available for quota tracking")
	}

	payload := fmt.Sprintf(`{"origin":"AI_EDITOR","profileArn":"%s","resourceType":"AGENTIC_REQUEST"}`, profileArn)

	req, err := http.NewRequest("POST", usageBaseURL, strings.NewReader(payload))
	if err != nil {
		return nil, "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("x-amz-target", "AmazonCodeWhispererService.GetUsageLimits")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("kiro api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, "", fmt.Errorf("kiro token expired or access denied (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("kiro api error %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read response: %w", err)
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, "", fmt.Errorf("parse response: %w", err)
	}

	return parseKiroQuotas(data)
}

func parseKiroQuotas(data map[string]any) ([]QuotaItem, string, error) {
	usageList, ok := data["usageBreakdownList"].([]any)
	if !ok || len(usageList) == 0 {
		return nil, "", fmt.Errorf("kiro connected but no usage breakdown returned")
	}

	resetAt := ""
	if rd, ok := data["nextDateReset"].(string); ok {
		resetAt = rd
	} else if rd, ok := data["resetDate"].(string); ok {
		resetAt = rd
	}

	// Check overage enabled
	overageEnabled := false
	if si, ok := data["subscriptionInfo"].(map[string]any); ok {
		if oe, ok := si["overageEnabled"].(bool); ok {
			overageEnabled = oe
		}
	}
	_ = overageEnabled // used for future display logic

	var quotas []QuotaItem
	for _, entry := range usageList {
		bd, ok := entry.(map[string]any)
		if !ok {
			continue
		}

		resourceType := "unknown"
		if rt, ok := bd["resourceType"].(string); ok {
			resourceType = strings.ToLower(rt)
		}

		used := getNumberField(bd, "currentUsageWithPrecision", "current_usage_with_precision")
		total := getNumberField(bd, "usageLimitWithPrecision", "usage_limit_with_precision")

		var remainingPct float64
		if total > 0 {
			remainingPct = ((total - used) / total) * 100
		}

		quotas = append(quotas, QuotaItem{
			Name:         resourceType,
			Used:         used,
			Total:        total,
			RemainingPct: remainingPct,
			ResetAt:      resetAt,
			Unlimited:    false,
		})

	}

	// Extract plan
	plan := "Kiro"
	if si, ok := data["subscriptionInfo"].(map[string]any); ok {
		if title, ok := si["subscriptionTitle"].(string); ok && title != "" {
			plan = title
		}
	}

	return quotas, plan, nil
}

// discoverKiroProfileArn tries to find a profile ARN via ListAvailableProfiles.
func discoverKiroProfileArn(accessToken, usageBaseURL string) string {
	payload := `{"origin":"AI_EDITOR"}`

	req, err := http.NewRequest("POST", usageBaseURL, strings.NewReader(payload))
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("x-amz-target", "AmazonCodeWhispererService.ListAvailableProfiles")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return ""
	}

	profiles, ok := data["profiles"].([]any)
	if !ok || len(profiles) == 0 {
		return ""
	}

	// Return the first profile ARN
	for _, p := range profiles {
		profile, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if arn, ok := profile["profileArn"].(string); ok && arn != "" {
			return arn
		}
		if arn, ok := profile["arn"].(string); ok && arn != "" {
			return arn
		}
	}

	return ""
}
