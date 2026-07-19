package quota

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const kiroDefaultBaseURL = "https://codewhisperer.us-east-1.amazonaws.com"

var (
	awsRegionPattern      = regexp.MustCompile(`^[a-z]{2}-[a-z]+-\d{1,2}$`)
	kiroProfileARNRe      = regexp.MustCompile(`^arn:aws:codewhisperer:([a-z0-9-]+):`)
	kiroProfileRegions    = map[string]struct{}{"us-east-1": {}, "eu-central-1": {}}
	kiroHTTPClient        = &http.Client{Timeout: 15 * time.Second}
	kiroCodeWhispererBase = kiroDefaultBaseURL
)

func stringAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func psdToStringMap(psd map[string]any) map[string]string {
	out := make(map[string]string, len(psd))
	for k, v := range psd {
		out[k] = stringAny(v)
	}
	return out
}

func normalizeRegion(region string) string {
	return strings.ToLower(strings.TrimSpace(region))
}

func regionFromKiroProfileArn(profileArn string) string {
	if profileArn == "" {
		return ""
	}
	matches := kiroProfileARNRe.FindStringSubmatch(profileArn)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

func resolveKiroRuntimeRegion(psd map[string]string) string {
	fromArn := regionFromKiroProfileArn(psd["profileArn"])
	if fromArn != "" && awsRegionPattern.MatchString(fromArn) {
		return fromArn
	}
	stored := normalizeRegion(psd["region"])
	if stored != "" {
		if _, ok := kiroProfileRegions[stored]; ok {
			return stored
		}
	}
	return "us-east-1"
}

func kiroRuntimeHost(region string) string {
	if region == "us-east-1" {
		return kiroCodeWhispererBase
	}
	return fmt.Sprintf("https://q.%s.amazonaws.com", region)
}

func buildKiroProfileDiscoveryRegions(storedRegion string) []string {
	stored := normalizeRegion(storedRegion)
	preferEU := regexp.MustCompile(`^(eu|af|me|il)-`).MatchString(stored)
	var regions []string
	if preferEU {
		regions = []string{"eu-central-1", "us-east-1"}
	} else {
		regions = []string{"us-east-1", "eu-central-1"}
	}
	if stored != "" && awsRegionPattern.MatchString(stored) {
		found := false
		for _, r := range regions {
			if r == stored {
				found = true
				break
			}
		}
		if !found {
			regions = append(regions, stored)
		}
	}
	return regions
}

func discoverKiroProfileArnAcrossRegions(accessToken, storedRegion string) string {
	if accessToken == "" {
		return ""
	}
	for _, region := range buildKiroProfileDiscoveryRegions(storedRegion) {
		if !awsRegionPattern.MatchString(region) {
			continue
		}
		payload := []byte(`{"maxResults":10}`)
		req, err := http.NewRequest(http.MethodPost, kiroRuntimeHost(region)+"/", bytes.NewReader(payload))
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/x-amz-json-1.0")
		req.Header.Set("x-amz-target", "AmazonCodeWhispererService.ListAvailableProfiles")
		req.Header.Set("Accept", "application/json")
		resp, err := kiroHTTPClient.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			continue
		}
		var data struct {
			Profiles []struct {
				ARN string `json:"arn"`
			} `json:"profiles"`
		}
		if err := json.Unmarshal(body, &data); err != nil {
			continue
		}
		var first string
		for _, p := range data.Profiles {
			if p.ARN == "" {
				continue
			}
			if first == "" {
				first = p.ARN
			}
			if regionFromKiroProfileArn(p.ARN) == region {
				return p.ARN
			}
		}
		if first != "" {
			return first
		}
	}
	return ""
}

func isSocialAuthKiroAccount(psd map[string]any) bool {
	if stringAny(psd["authMethod"]) != "imported" {
		return false
	}
	provider := strings.ToLower(stringAny(psd["provider"]))
	return provider == "google" || provider == "github"
}

func kiroAuthHeaders(accessToken string, psd map[string]any) map[string]string {
	headers := map[string]string{
		"Authorization": "Bearer " + accessToken,
		"Accept":        "application/json",
	}
	if stringAny(psd["authMethod"]) == "api_key" {
		headers["tokentype"] = "API_KEY"
	}
	if stringAny(psd["authMethod"]) == "external_idp" {
		headers["TokenType"] = "EXTERNAL_IDP"
	}
	return headers
}

func isKiroOverageEnabled(data map[string]any) bool {
	if oe, ok := data["overageEnabled"].(bool); ok && oe {
		return true
	}
	if oc, ok := data["overageConfiguration"].(map[string]any); ok {
		if status := strings.ToUpper(stringAny(oc["overageStatus"])); status == "ENABLED" {
			return true
		}
		if oe, ok := oc["overageEnabled"].(bool); ok && oe {
			return true
		}
	}
	return false
}

func parseKiroResetAt(data map[string]any) string {
	if s := stringAny(data["nextDateReset"]); s != "" {
		return s
	}
	return stringAny(data["resetDate"])
}

func parseKiroQuotas(data map[string]any) ([]QuotaItem, string, error) {
	var usageList []any
	if raw, ok := data["usageBreakdownList"].([]any); ok {
		usageList = raw
	} else if raw, ok := data["usageBreakdownList"].([]map[string]any); ok {
		usageList = make([]any, len(raw))
		for i, v := range raw {
			usageList[i] = v
		}
	}
	if len(usageList) == 0 {
		return nil, "", fmt.Errorf("kiro connected but no usage breakdown returned")
	}
	resetAt := parseKiroResetAt(data)
	overageEnabled := isKiroOverageEnabled(data)
	plan := "Kiro"
	if si, ok := data["subscriptionInfo"].(map[string]any); ok {
		if title := stringAny(si["subscriptionTitle"]); title != "" {
			plan = title
		}
	}

	var quotas []QuotaItem
	for _, entry := range usageList {
		bd, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		resourceType := strings.ToLower(stringAny(bd["resourceType"]))
		if resourceType == "" {
			resourceType = "unknown"
		}
		used := getNumberField(bd, "currentUsageWithPrecision", "current_usage_with_precision")
		total := getNumberField(bd, "usageLimitWithPrecision", "usage_limit_with_precision")
		remainingPct := 0.0
		if overageEnabled {
			remainingPct = 100
		} else if total > 0 {
			remainingPct = ((total - used) / total) * 100
		}
		quotas = append(quotas, QuotaItem{
			Name:         resourceType,
			Used:         used,
			Total:        total,
			RemainingPct: remainingPct,
			ResetAt:      resetAt,
			Unlimited:    overageEnabled,
		})
		if fti, ok := bd["freeTrialInfo"].(map[string]any); ok && len(fti) > 0 {
			freeUsed := getNumberField(fti, "currentUsageWithPrecision", "current_usage_with_precision")
			freeTotal := getNumberField(fti, "usageLimitWithPrecision", "usage_limit_with_precision")
			freeRemainingPct := 0.0
			if overageEnabled {
				freeRemainingPct = 100
			} else if freeTotal > 0 {
				freeRemainingPct = ((freeTotal - freeUsed) / freeTotal) * 100
			}
			quotas = append(quotas, QuotaItem{
				Name:         resourceType + "_freetrial",
				Used:         freeUsed,
				Total:        freeTotal,
				RemainingPct: freeRemainingPct,
				ResetAt:      resetAt,
				Unlimited:    overageEnabled,
			})
		}
	}
	return quotas, plan, nil
}

type kiroUsageAttempt struct {
	name string
	fn   func() (*http.Response, error)
}

func runKiroUsageAttempts(attempts []kiroUsageAttempt) (map[string]any, bool, error) {
	var sawAuthError bool
	var lastErr error
	for _, a := range attempts {
		resp, err := a.fn()
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			sawAuthError = true
			lastErr = fmt.Errorf("%s: Kiro API error (%d): %s", a.name, resp.StatusCode, string(body))
			continue
		}
		if resp.StatusCode != 200 {
			lastErr = fmt.Errorf("%s: Kiro API error (%d): %s", a.name, resp.StatusCode, string(body))
			continue
		}
		var data map[string]any
		if err := json.Unmarshal(body, &data); err != nil {
			lastErr = fmt.Errorf("%s: parse response: %w", a.name, err)
			continue
		}
		return data, sawAuthError, nil
	}
	return nil, sawAuthError, lastErr
}

func urlEncode(s string) string {
	out := strings.Builder{}
	for _, r := range s {
		switch r {
		case '&':
			out.WriteString("%26")
		case '=':
			out.WriteString("%3D")
		case '+':
			out.WriteString("%2B")
		case ' ':
			out.WriteString("%20")
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// fetchKiroQuota fetches quota from AWS CodeWhisperer (Kiro) API.
// It returns quotas, plan, optional message, and an error.
func fetchKiroQuota(accessToken string, psd map[string]any) ([]QuotaItem, string, string, error) {
	if psd == nil {
		psd = map[string]any{}
	}
	psdStr := psdToStringMap(psd)

	isApiKey := psdStr["authMethod"] == "api_key"
	profileArn := psdStr["profileArn"]

	if profileArn == "" && !isApiKey {
		profileArn = discoverKiroProfileArnAcrossRegions(accessToken, psdStr["region"])
	}

	if profileArn == "" && !isApiKey {
		return nil, "", "Kiro connected. Profile ARN not available for quota tracking.", nil
	}

	region := resolveKiroRuntimeRegion(psdStr)
	usageBaseURL := kiroRuntimeHost(region)
	qBaseURL := fmt.Sprintf("https://q.%s.amazonaws.com", region)

	authHeaders := kiroAuthHeaders(accessToken, psd)

	payload := map[string]any{
		"origin":       "AI_EDITOR",
		"resourceType": "AGENTIC_REQUEST",
	}
	if profileArn != "" {
		payload["profileArn"] = profileArn
	}
	payloadBytes, _ := json.Marshal(payload)

	usageParams := "isEmailRequired=true&origin=AI_EDITOR&resourceType=AGENTIC_REQUEST"
	qParams := "origin=AI_EDITOR&resourceType=AGENTIC_REQUEST"
	if profileArn != "" {
		qParams += "&profileArn=" + urlEncode(profileArn)
	}

	attempts := []kiroUsageAttempt{
		{
			name: "codewhisperer-post",
			fn: func() (*http.Response, error) {
				req, err := http.NewRequest(http.MethodPost, usageBaseURL+"/", bytes.NewReader(payloadBytes))
				if err != nil {
					return nil, err
				}
				for k, v := range authHeaders {
					req.Header.Set(k, v)
				}
				req.Header.Set("Content-Type", "application/x-amz-json-1.0")
				req.Header.Set("x-amz-target", "AmazonCodeWhispererService.GetUsageLimits")
				return kiroHTTPClient.Do(req)
			},
		},
		{
			name: "codewhisperer-get",
			fn: func() (*http.Response, error) {
				url := kiroCodeWhispererBase + "/getUsageLimits?" + usageParams
				req, err := http.NewRequest(http.MethodGet, url, nil)
				if err != nil {
					return nil, err
				}
				for k, v := range authHeaders {
					req.Header.Set(k, v)
				}
				req.Header.Set("x-amz-user-agent", "aws-sdk-js/1.0.0 KiroIDE")
				req.Header.Set("User-Agent", "aws-sdk-js/1.0.0 KiroIDE")
				return kiroHTTPClient.Do(req)
			},
		},
		{
			name: "q-get",
			fn: func() (*http.Response, error) {
				url := qBaseURL + "/getUsageLimits?" + qParams
				req, err := http.NewRequest(http.MethodGet, url, nil)
				if err != nil {
					return nil, err
				}
				for k, v := range authHeaders {
					req.Header.Set(k, v)
				}
				return kiroHTTPClient.Do(req)
			},
		},
	}

	data, sawAuthError, err := runKiroUsageAttempts(attempts)
	if err != nil {
		if sawAuthError && isSocialAuthKiroAccount(psd) {
			return nil, "", "Kiro quota API authentication expired. Chat may still work.", nil
		}
		return nil, "", "", err
	}

	quotas, plan, err := parseKiroQuotas(data)
	if err != nil {
		return nil, "", "", err
	}
	return quotas, plan, "", nil
}
