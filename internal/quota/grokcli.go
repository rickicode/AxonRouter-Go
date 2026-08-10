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
	grokCliBillingURL   = "https://cli-chat-proxy.grok.com/v1/billing?format=credits"
	grokCliUserURL      = "https://cli-chat-proxy.grok.com/v1/user?include=subscription"
	grokCliTaskUsageURL = "https://grok.com/rest/tasks/usage"
	grokCliVersion      = "0.2.99"
	grokCliUserAgent    = "grok-shell/" + grokCliVersion + " (linux; x86_64)"
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
	taskCh := make(chan grokCliResult, 1)

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		billingCh <- fetchGrokCliJSON(client, grokCliBillingURL, headers)
	}()
	go func() {
		defer wg.Done()
		userCh <- fetchGrokCliJSON(client, grokCliUserURL, headers)
	}()
	go func() {
		defer wg.Done()
		taskCh <- fetchGrokCliJSON(client, grokCliTaskUsageURL, headers)
	}()
	wg.Wait()
	close(billingCh)
	close(userCh)
	close(taskCh)

	billing := <-billingCh
	user := <-userCh
	task := <-taskCh
	if billing.err != nil {
		return nil, "", billing.err
	}

	var billingMap, userMap, taskMap map[string]any
	json.Unmarshal(billing.body, &billingMap)
	json.Unmarshal(user.body, &userMap)
	json.Unmarshal(task.body, &taskMap)

	plan, quotas := parseGrokCliBilling(billingMap, userMap, taskMap)
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
		"x-grok-cli-version":       grokCliVersion,
		"x-grok-client-version":    grokCliVersion,
		"x-grok-client-surface":    "grok-cli",
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

// grokCreditAmounts extracts a used/total credit pair from a set of possible
// bag locations used by Grok CLI billing payloads.
func grokCreditAmounts(billing, config map[string]any) (used, total float64) {
	sources := []map[string]any{billing, config}
	keys := []string{"credits", "creditBalance", "usage", "includedCredits", "subscriptionCredits", "weeklyCredits", "sharedPool"}
	for _, src := range sources {
		if src == nil {
			continue
		}
		for _, k := range keys {
			if v, ok := src[k]; ok {
				if u, t := grokCreditBagAmounts(v); t > 0 {
					return u, t
				}
			}
		}
	}
	return 0, 0
}

func grokCreditBagAmounts(v any) (used, total float64) {
	if items, ok := v.([]any); ok && len(items) > 0 {
		// Pick the first item with a positive total.
		for _, raw := range items {
			if u, t := grokCreditBagAmounts(raw); t > 0 {
				return u, t
			}
		}
		return 0, 0
	}
	m, ok := v.(map[string]any)
	if !ok {
		return 0, 0
	}
	total = unwrapGrokVal(m["total"])
	if total <= 0 {
		total = unwrapGrokVal(m["limit"])
	}
	if total <= 0 {
		total = unwrapGrokVal(m["cap"])
	}
	if total <= 0 {
		total = unwrapGrokVal(m["allocation"])
	}
	if total <= 0 {
		total = unwrapGrokVal(m["amount"])
	}
	used = unwrapGrokVal(m["used"])
	if used <= 0 {
		used = unwrapGrokVal(m["spent"])
	}
	if used <= 0 {
		used = unwrapGrokVal(m["consumed"])
	}
	if used <= 0 {
		used = unwrapGrokVal(m["usage"])
	}
	if used <= 0 && total > 0 {
		remaining := unwrapGrokVal(m["remaining"])
		if remaining <= 0 {
			remaining = unwrapGrokVal(m["balance"])
		}
		if remaining <= 0 {
			remaining = unwrapGrokVal(m["left"])
		}
		if remaining > 0 {
			used = total - remaining
			if used < 0 {
				used = 0
			}
		}
	}
	return used, total
}

func grokProductUsageQuota(item map[string]any, resetAt string) *QuotaItem {
	name := getStringField(item, "product", "name", "productName")
	if name == "" {
		return nil
	}
	used, total := grokCreditBagAmounts(item)
	if total <= 0 {
		return nil
	}
	return &QuotaItem{
		Name:         name,
		Used:         used,
		Total:        total,
		RemainingPct: ((total - used) / total) * 100,
		ResetAt:      resetAt,
	}
}

func grokTaskUsageQuota(name string, taskUsage map[string]any, usageKey, limitKey, resetAt string) *QuotaItem {
	usageVal := unwrapGrokVal(taskUsage[usageKey])
	limitVal := unwrapGrokVal(taskUsage[limitKey])
	if limitVal <= 0 {
		return nil
	}
	return &QuotaItem{
		Name:         name,
		Used:         usageVal,
		Total:        limitVal,
		RemainingPct: ((limitVal - usageVal) / limitVal) * 100,
		ResetAt:      resetAt,
	}
}

func parseGrokCliBilling(billing, user, taskUsage map[string]any) (string, []QuotaItem) {
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

	quotas := make([]QuotaItem, 0)

	// Legacy monthly included fields, kept for backward compatibility.
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

	// creditUsagePercent mirrors Grok CLI web/app overview: weekly pool usage.
	if creditPct, ok := grokNum(config, billing, "creditUsagePercent", "credit_usage_percent", "usagePercent"); ok && creditPct > 0 {
		quotas = append(quotas, makeGrokCliQuota("Weekly credits", creditPct, 100, periodEnd))
	}

	// Try several possible credit bag locations (config, billing top-level).
	if used, total := grokCreditAmounts(billing, config); total > 0 {
		quotas = append(quotas, makeGrokCliQuota("Weekly credits", used, total, periodEnd))
	}

	// Product-level usage (Grok, DeepSearch, etc.).
	productUsage := getSliceField(config, "productUsage", "product_usage")
	for _, raw := range productUsage {
		if item, ok := raw.(map[string]any); ok {
			if q := grokProductUsageQuota(item, periodEnd); q != nil {
				quotas = append(quotas, *q)
			}
		}
	}

	// On-demand cap.
	onDemandCap, _ := grokNum(config, billing, "onDemandCap")
	onDemandUsed, _ := grokNum(config, billing, "onDemandUsed")
	if onDemandCap > 0 {
		used := onDemandUsed
		if used < 0 {
			used = 0
		}
		quotas = append(quotas, makeGrokCliQuota("On-demand", used, onDemandCap, periodEnd))
	}

	// Prepaid balance (remaining money, not a usage-limit; show as total).
	prepaid, _ := grokNum(config, billing, "prepaidBalance")
	if prepaid > 0 {
		quotas = append(quotas, QuotaItem{
			Name: "Prepaid", Used: 0, Total: prepaid,
			RemainingPct: 100, ResetAt: "", Unlimited: false,
		})
	}

	// Task-based limits from grok.com/rest/tasks/usage (frequent / occasional tiers).
	if taskUsage != nil {
		if q := grokTaskUsageQuota("Frequent", taskUsage, "frequentUsage", "frequentLimit", periodEnd); q != nil {
			quotas = append(quotas, *q)
		}
		if q := grokTaskUsageQuota("Occasional", taskUsage, "occasionalUsage", "occasionalLimit", periodEnd); q != nil {
			quotas = append(quotas, *q)
		}
	}

	// Fallback unlimited indicator when we know the account is paid but no
	// concrete limits were returned.
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
