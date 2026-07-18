package quota

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	grokCliBillingURL = "https://cli-chat-proxy.grok.com/v1/billing?format=credits"
	grokCliUserURL    = "https://cli-chat-proxy.grok.com/v1/user?include=subscription"
	grokCliVersion    = "0.2.99"
	grokCliUserAgent  = "grok-shell/" + grokCliVersion + " (linux; x86_64)"
)

type grokCliResult struct {
	body []byte
	err  error
}

// fetchGrokCliQuota fetches Grok CLI / Grok Build credit quotas from the
// cli-chat-proxy billing and user endpoints. Modeled on
// 9router/open-sse/services/usage/grok-cli.js.
func fetchGrokCliQuota(accessToken string, psd map[string]any) ([]QuotaItem, string, error) {
	if accessToken == "" {
		return nil, "", fmt.Errorf("grok-cli access token not available")
	}

	headers := grokCliHeaders(accessToken, psd)
	client := &http.Client{Timeout: 15 * time.Second}

	billingCh := make(chan grokCliResult, 1)
	userCh := make(chan grokCliResult, 1)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		billingCh <- fetchGrokCliJSON(client, grokCliBillingURL, headers)
	}()
	go func() {
		defer wg.Done()
		userCh <- fetchGrokCliJSON(client, grokCliUserURL, headers)
	}()
	wg.Wait()
	close(billingCh)
	close(userCh)

	billing := <-billingCh
	user := <-userCh
	if billing.err != nil {
		return nil, "", billing.err
	}

	var billingMap, userMap map[string]any
	json.Unmarshal(billing.body, &billingMap)
	json.Unmarshal(user.body, &userMap)

	plan, quotas := parseGrokCliBilling(billingMap, userMap)
	return quotas, plan, nil
}

func fetchGrokCliJSON(client *http.Client, url string, headers map[string]string) grokCliResult {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return grokCliResult{err: fmt.Errorf("create request: %w", err)}
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return grokCliResult{err: fmt.Errorf("request failed: %w", err)}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return grokCliResult{err: fmt.Errorf("read response: %w", err)}
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return grokCliResult{err: fmt.Errorf("grok-cli token expired or access denied (HTTP %d)", resp.StatusCode)}
	}
	if resp.StatusCode != http.StatusOK {
		return grokCliResult{err: fmt.Errorf("grok-cli api error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	}
	return grokCliResult{body: body}
}

func grokCliHeaders(accessToken string, psd map[string]any) map[string]string {
	email := ""
	userID := ""
	if psd != nil {
		if s, ok := psd["email"].(string); ok {
			email = s
		}
		if s, ok := psd["sub"].(string); ok && s != "" {
			userID = s
		} else if s, ok := psd["principalId"].(string); ok {
			userID = s
		}
	}
	h := map[string]string{
		"Authorization":            "Bearer " + accessToken,
		"Accept":                   "application/json",
		"Content-Type":             "application/json",
		"X-XAI-Token-Auth":         "xai-grok-cli",
		"x-grok-client-version":    grokCliVersion,
		"x-grok-client-identifier": "grok-shell",
		"x-grok-client-mode":       "headless",
		"User-Agent":               grokCliUserAgent,
	}
	if email != "" {
		h["x-email"] = email
	}
	if userID != "" {
		h["x-userid"] = userID
	}
	return h
}

func parseGrokCliBilling(billing, user map[string]any) (string, []QuotaItem) {
	if billing == nil {
		billing = map[string]any{}
	}
	if user == nil {
		user = map[string]any{}
	}

	config := billing
	if cfg := getMapField(billing, "config"); cfg != nil {
		config = cfg
	}

	periodEnd := parseGrokCliResetTime(
		getStringField(billing, "billingPeriodEnd", "billing_period_end"),
		getStringField(config, "billingPeriodEnd", "billing_period_end"),
		getStringField(getMapField(config, "currentPeriod"), "end"),
		getStringField(billing, "resetAt", "resetsAt", "periodEnd"),
	)

	tier := grokCliSubscriptionTier(user, config)
	subscriptionAccess := tier != "" && !strings.EqualFold(tier, "free") && !strings.EqualFold(tier, "none")

	var quotas []QuotaItem

	monthlyLimit, _ := grokNum(config, billing, "monthlyLimit", "monthly_limit")
	includedUsed, _ := grokNum(config, billing, "includedUsed", "included_used")
	totalUsed, _ := grokNum(config, billing, "totalUsed", "total_used")
	if monthlyLimit > 0 {
		used := includedUsed
		if used <= 0 && totalUsed > 0 {
			used = totalUsed
		}
		quotas = append(quotas, makeGrokCliQuota("Monthly included", used, monthlyLimit, periodEnd))
	}

	onDemandCap, _ := grokNum(config, billing, "onDemandCap")
	onDemandUsed, onDemandOK := grokNum(config, billing, "onDemandUsed")
	if onDemandCap > 0 {
		used := onDemandUsed
		if used < 0 {
			used = 0
		}
		quotas = append(quotas, makeGrokCliQuota("On-demand", used, onDemandCap, periodEnd))
	} else if !subscriptionAccess && onDemandCap == 0 && onDemandOK {
		// Free/promo account with explicit onDemand data and no cap => depleted.
		quotas = append(quotas, QuotaItem{
			Name: "On-demand", Used: 1, Total: 1,
			RemainingPct: 0, ResetAt: periodEnd, Unlimited: false,
		})
	}

	prepaid, _ := grokNum(config, billing, "prepaidBalance")
	if prepaid > 0 {
		quotas = append(quotas, QuotaItem{
			Name: "Prepaid", Used: 0, Total: prepaid,
			RemainingPct: 100, ResetAt: "", Unlimited: false,
		})
	}

	if len(quotas) == 0 && subscriptionAccess {
		quotas = append(quotas, QuotaItem{
			Name: "Included", Used: 0, Total: 0,
			RemainingPct: 100, Unlimited: true,
		})
	}

	plan := grokCliResolvePlan(user, config, tier)
	return plan, quotas
}

func makeGrokCliQuota(name string, used, total float64, resetAt string) QuotaItem {
	if total <= 0 {
		return QuotaItem{Name: name, Used: used, Total: 0, RemainingPct: 100, ResetAt: resetAt, Unlimited: true}
	}
	remaining := total - used
	if remaining < 0 {
		remaining = 0
	}
	return QuotaItem{
		Name: name, Used: used, Total: total,
		RemainingPct: (remaining / total) * 100,
		ResetAt:      resetAt, Unlimited: false,
	}
}

func grokCliResolvePlan(user, config map[string]any, tier string) string {
	if tier != "" {
		return grokCliTitleCase(tier)
	}
	if v, ok := user["hasGrokCodeAccess"]; ok {
		if b, ok := v.(bool); ok && b {
			return "Grok Code"
		}
	}
	return "Grok Build"
}

func grokCliSubscriptionTier(user, config map[string]any) string {
	if s := getStringField(user, "subscriptionTier", "subscription_tier"); s != "" {
		return strings.TrimSpace(s)
	}
	if s := getStringField(getMapField(user, "subscription"), "tier"); s != "" {
		return strings.TrimSpace(s)
	}
	if s := getStringField(config, "subscriptionTier", "subscription_tier"); s != "" {
		return strings.TrimSpace(s)
	}
	return ""
}

func parseGrokCliResetTime(candidates ...string) string {
	for _, c := range candidates {
		if c == "" {
			continue
		}
		for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02"} {
			if t, err := time.Parse(layout, c); err == nil {
				return t.UTC().Format(time.RFC3339)
			}
		}
		if f, ok := grokCliParseFloat(c); ok {
			ts := int64(f)
			if f > 1e12 {
				ts = int64(f / 1000)
			}
			return time.Unix(ts, 0).UTC().Format(time.RFC3339)
		}
	}
	return ""
}

// grokNum searches primary then fallback maps for the first matching key and
// unwraps protobuf-json { val: n } wrappers while reporting presence.
func grokNum(primary, fallback map[string]any, keys ...string) (float64, bool) {
	for _, m := range []map[string]any{primary, fallback} {
		if m == nil {
			continue
		}
		for _, k := range keys {
			if v, ok := m[k]; ok {
				return unwrapGrokVal(v), true
			}
		}
	}
	return 0, false
}

func unwrapGrokVal(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case string:
		if f, ok := grokCliParseFloat(n); ok {
			return f
		}
	case map[string]any:
		if val, ok := n["val"]; ok {
			return unwrapGrokVal(val)
		}
	}
	return 0
}

func grokCliParseFloat(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

func grokCliTitleCase(s string) string {
	if s == "" {
		return s
	}
	s = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(s, "_", " "), "-", " "))
	parts := strings.Fields(s)
	for i := range parts {
		if len(parts[i]) > 0 {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, " ")
}
