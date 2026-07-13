package quota

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Codex emits per-window usage quotas on real chat/responses traffic via these
// headers (OmniRoute open-sse/executors/codex/quota.ts parseCodexQuotaHeaders):
//
//	x-codex-5h-usage    tokens used in the trailing 5h window
//	x-codex-5h-limit    token limit for the 5h window
//	x-codex-5h-reset-at ISO timestamp when the 5h window resets
//	x-codex-7d-usage    tokens used in the trailing 7d window
//	x-codex-7d-limit    token limit for the 7d window
//	x-codex-7d-reset-at ISO timestamp when the 7d window resets
//
// The /wham/usage endpoint only returns the primary (session) window and may
// leave secondary_window null, so the dual-window 5h/7d view must be sourced
// from the response headers (plan item C.3).
//
// For backward compatibility we also parse generic OpenAI-style X-RateLimit-*
// headers when no Codex-specific headers are present; that provides a usable
// per-request window view for other OpenAI-compatible responses.
func ParseCodexQuotaHeaders(h http.Header) []QuotaItem {
	var codexQuotas []QuotaItem
	if q := parseCodexHeaderWindow(h, "5h", "5h"); q.Name != "" {
		codexQuotas = append(codexQuotas, q)
	}
	if q := parseCodexHeaderWindow(h, "7d", "7d"); q.Name != "" {
		codexQuotas = append(codexQuotas, q)
	}
	if len(codexQuotas) > 0 {
		return codexQuotas
	}

	var quotas []QuotaItem
	if q := parseGenericRateLimitWindow(h, "requests"); q.Name != "" {
		quotas = append(quotas, q)
	}
	if q := parseGenericRateLimitWindow(h, "tokens"); q.Name != "" {
		quotas = append(quotas, q)
	}
	return quotas
}

func parseGenericRateLimitWindow(h http.Header, kind string) QuotaItem {
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
			ts := epoch
			if ts > 1e12 {
				ts /= 1000
			}
			qi.ResetAt = time.Unix(int64(ts), 0).UTC().Format(time.RFC3339)
		}
	}
	return qi
}

func parseCodexHeaderWindow(h http.Header, key, name string) QuotaItem {
	usageStr := h.Get("x-codex-" + key + "-usage")
	limitStr := h.Get("x-codex-" + key + "-limit")
	resetStr := h.Get("x-codex-" + key + "-reset-at")
	if usageStr == "" && limitStr == "" {
		return QuotaItem{}
	}
	usage, _ := strconv.ParseFloat(strings.TrimSpace(usageStr), 64)
	limit, _ := strconv.ParseFloat(strings.TrimSpace(limitStr), 64)

	qi := QuotaItem{
		Name:      name,
		Used:      usage,
		Total:     limit,
		Scope:     "codex",
		Unlimited: false,
	}
	if limit > 0 {
		qi.RemainingPct = ((limit - usage) / limit) * 100
	}
	if resetStr != "" {
		qi.ResetAt = resetStr
	}
	return qi
}

// SaveCodexHeaderQuota merges the dual-window 5h/7d headers from a live Codex
// response into the connection's cached quota and upserts it. Existing
// session/secondary windows (from /wham/usage) are preserved; only the 5h/7d
// windows are refreshed from the header source.
func SaveCodexHeaderQuota(db *sql.DB, connID, providerID, connName, plan string, headers http.Header) {
	snap := ParseCodexQuotaHeaders(headers)
	if len(snap) == 0 {
		return
	}

	existing := readCachedQuotas(db, connID)
	merged := mergeCodexQuotas(existing, snap)

	now := time.Now().Unix()
	saveQuotaCacheEntry(db, connID, connID, providerID, connName, plan, merged, "", now, now)
}

func readCachedQuotas(db *sql.DB, connID string) []QuotaItem {
	var raw string
	err := db.QueryRow(`SELECT quotas FROM quota_cache WHERE id = ?`, connID).Scan(&raw)
	if err != nil || raw == "" {
		return nil
	}
	var items []QuotaItem
	if json.Unmarshal([]byte(raw), &items) != nil {
		return nil
	}
	return items
}

// mergeCodexQuotas keeps non-5h/7d windows from the existing cache and overlays
// the freshly parsed 5h/7d windows from the live response.
func mergeCodexQuotas(existing, snap []QuotaItem) []QuotaItem {
	keep := make([]QuotaItem, 0, len(existing)+len(snap))
	for _, q := range existing {
		if q.Name == "5h" || q.Name == "7d" {
			continue
		}
		keep = append(keep, q)
	}
	keep = append(keep, snap...)
	return keep
}

// CodexQuotaSnapshot is the structured dual-window view parsed from headers.
// Exposed for callers (e.g. failover cooldown math) that need raw values.
type CodexQuotaSnapshot struct {
	Usage5h   float64
	Limit5h   float64
	ResetAt5h string
	Usage7d   float64
	Limit7d   float64
	ResetAt7d string
}

// SnapshotFromHeaders parses a CodexQuotaSnapshot from response headers, or nil
// when no dual-window headers are present.
func SnapshotFromHeaders(h http.Header) *CodexQuotaSnapshot {
	snap := &CodexQuotaSnapshot{
		Usage5h:   atof(h.Get("x-codex-5h-usage")),
		Limit5h:   atof(h.Get("x-codex-5h-limit")),
		ResetAt5h: h.Get("x-codex-5h-reset-at"),
		Usage7d:   atof(h.Get("x-codex-7d-usage")),
		Limit7d:   atof(h.Get("x-codex-7d-limit")),
		ResetAt7d: h.Get("x-codex-7d-reset-at"),
	}
	if snap.Limit5h == 0 && snap.Limit7d == 0 {
		return nil
	}
	return snap
}

func atof(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}
