package quota

import (
	"net/http"
	"strconv"
	"time"
)

// ParseCodexQuotaHeaders converts Codex/OpenAI rate-limit headers into
// QuotaItems. Headers are typically of the form:
//
//	x-ratelimit-limit-requests: 1000
//	x-ratelimit-remaining-requests: 999
//	x-ratelimit-reset-requests: 2026-07-12T00:00:00Z
//	x-ratelimit-limit-tokens: 1000000
//	x-ratelimit-remaining-tokens: 999999
//	x-ratelimit-reset-tokens: 2026-07-12T00:00:00Z
func ParseCodexQuotaHeaders(h http.Header) []QuotaItem {
	var quotas []QuotaItem
	if q := parseHeaderWindow(h, "requests"); q.Name != "" {
		quotas = append(quotas, q)
	}
	if q := parseHeaderWindow(h, "tokens"); q.Name != "" {
		quotas = append(quotas, q)
	}
	return quotas
}

func parseHeaderWindow(h http.Header, kind string) QuotaItem {
	prefix := "X-Ratelimit-"
	limitStr := h.Get(prefix + "Limit-" + kind)
	remainingStr := h.Get(prefix + "Remaining-" + kind)
	resetStr := h.Get(prefix + "Reset-" + kind)
	if limitStr == "" && remainingStr == "" {
		return QuotaItem{}
	}
	limit, _ := strconv.ParseFloat(limitStr, 64)
	remaining, _ := strconv.ParseFloat(remainingStr, 64)
	qi := QuotaItem{
		Name:      "RateLimit " + kind,
		Used:      remaining,
		Total:     limit,
		Scope:     "codex",
		Unlimited: false,
	}
	if limit > 0 {
		qi.RemainingPct = (remaining / limit) * 100
		qi.Used = limit - remaining
	}
	if resetStr != "" {
		if t, err := time.Parse(time.RFC3339, resetStr); err == nil {
			qi.ResetAt = t.Format(time.RFC3339)
		} else if epoch, err := strconv.ParseFloat(resetStr, 64); err == nil {
			// Some providers send epoch seconds; others send epoch milliseconds.
			ts := epoch
			if ts > 1e12 {
				ts /= 1000
			}
			qi.ResetAt = time.Unix(int64(ts), 0).UTC().Format(time.RFC3339)
		}
	}
	return qi
}
