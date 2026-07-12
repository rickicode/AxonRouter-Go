package quota

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseCodexQuotas_PercentageWindows(t *testing.T) {
	rateLimit := map[string]any{
		"primary_window":   map[string]any{"used_percent": 80.0, "reset_at": 1700000000000.0},
		"secondary_window": map[string]any{"used_percent": 30.0, "reset_after_seconds": 3600.0},
	}
	quotas := parseCodexQuotas(rateLimit, nil)
	if len(quotas) != 2 {
		t.Fatalf("got %d quotas, want 2", len(quotas))
	}
	if quotas[0].RemainingPct != 20 {
		t.Errorf("primary remaining = %f", quotas[0].RemainingPct)
	}
	if quotas[1].RemainingPct != 70 {
		t.Errorf("secondary remaining = %f", quotas[1].RemainingPct)
	}
}

func TestParseCodexQuotas_AbsoluteWindows(t *testing.T) {
	rateLimit := map[string]any{
		"primary_window": map[string]any{"used": 75.0, "limit": 100.0},
	}
	quotas := parseCodexQuotas(rateLimit, nil)
	if len(quotas) != 1 {
		t.Fatalf("got %d quotas", len(quotas))
	}
	q := quotas[0]
	if q.Used != 75 || q.Total != 100 {
		t.Errorf("used/total = %f/%f", q.Used, q.Total)
	}
	if q.RemainingPct != 25 {
		t.Errorf("remaining pct = %f, want 25", q.RemainingPct)
	}
}

func TestParseCodexQuotas_SparkScope(t *testing.T) {
	data := map[string]any{
		"additional_rate_limits": []any{
			map[string]any{
				"limit_name": "gpt_5_3_codex_spark",
				"rate_limit": map[string]any{"primary_window": map[string]any{"used_percent": 10.0}},
			},
		},
	}
	quotas := parseCodexQuotas(nil, data)
	if len(quotas) != 1 || quotas[0].Scope != "spark" {
		t.Fatalf("expected one spark quota, got %+v", quotas)
	}
}

func TestParseCodexQuotas_CodeReview(t *testing.T) {
	data := map[string]any{
		"code_review_rate_limit": map[string]any{
			"primary_window": map[string]any{"used_percent": 50.0},
		},
	}
	quotas := parseCodexQuotas(nil, data)
	if len(quotas) != 1 || !strings.Contains(quotas[0].Name, "Code Review") {
		t.Fatalf("expected code review quota, got %+v", quotas)
	}
}

func TestCodexQuotaCooldown_ExhaustedSession(t *testing.T) {
	reset := time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339)
	quotas := []QuotaItem{{Name: "Session", RemainingPct: 0, ResetAt: reset, Scope: "codex"}}
	active, until, reason := CodexQuotaCooldown(quotas)
	if !active {
		t.Fatal("expected active cooldown")
	}
	if !until.After(time.Now()) {
		t.Errorf("until = %v", until)
	}
	if reason == "" {
		t.Error("expected reason")
	}
}

func TestCodexQuotaCooldown_UsesEarliestReset(t *testing.T) {
	reset1 := time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339)
	reset2 := time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339)
	quotas := []QuotaItem{
		{Name: "Weekly", RemainingPct: 0, ResetAt: reset1, Scope: "codex"},
		{Name: "Session", RemainingPct: 0, ResetAt: reset2, Scope: "codex"},
	}
	_, until, _ := CodexQuotaCooldown(quotas)
	want, _ := time.Parse(time.RFC3339, reset2)
	if !until.Equal(want) {
		t.Errorf("until = %v, want %v", until, want)
	}
}

func TestCodexQuotaCooldown_NoCooldownWhenRemaining(t *testing.T) {
	quotas := []QuotaItem{{Name: "Session", RemainingPct: 50.0, Scope: "codex"}}
	active, _, _ := CodexQuotaCooldown(quotas)
	if active {
		t.Error("expected no cooldown")
	}
}

func TestCodexQuotaCooldown_DefaultsTo60Seconds(t *testing.T) {
	quotas := []QuotaItem{{Name: "Session", RemainingPct: 0.0, Scope: "codex"}}
	active, until, _ := CodexQuotaCooldown(quotas)
	if !active {
		t.Fatal("expected cooldown")
	}
	maxUntil := time.Now().Add(75 * time.Second)
	if until.After(maxUntil) {
		t.Errorf("until too far in future: %v", until)
	}
}

func TestParseCodexQuotaHeaders_BothWindows(t *testing.T) {
	h := http.Header{}
	h.Set("X-Ratelimit-Limit-Requests", "100")
	h.Set("X-Ratelimit-Remaining-Requests", "90")
	h.Set("X-Ratelimit-Reset-Requests", time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	h.Set("X-Ratelimit-Limit-Tokens", "10000")
	h.Set("X-Ratelimit-Remaining-Tokens", "5000")
	qs := ParseCodexQuotaHeaders(h)
	if len(qs) != 2 {
		t.Fatalf("got %d quotas", len(qs))
	}
	if qs[0].RemainingPct != 90 {
		t.Errorf("requests remaining pct = %f", qs[0].RemainingPct)
	}
	if qs[1].RemainingPct != 50 {
		t.Errorf("tokens remaining pct = %f", qs[1].RemainingPct)
	}
}

func TestParseCodexQuotaHeaders_EpochReset(t *testing.T) {
	h := http.Header{}
	h.Set("X-Ratelimit-Limit-Requests", "10")
	h.Set("X-Ratelimit-Remaining-Requests", "5")
	h.Set("X-Ratelimit-Reset-Requests", "1893456000")
	qs := ParseCodexQuotaHeaders(h)
	if len(qs) != 1 {
		t.Fatalf("got %d quotas", len(qs))
	}
	if qs[0].ResetAt == "" {
		t.Error("expected reset_at parsed")
	}
}

func TestParseCodexQuotaHeaders_NoHeaders(t *testing.T) {
	qs := ParseCodexQuotaHeaders(http.Header{})
	if len(qs) != 0 {
		t.Errorf("got %d quotas", len(qs))
	}
}

func TestQuotaCache(t *testing.T) {
	ClearQuotaCache("")
	cq := ConnectionQuota{ConnectionID: "c1", FetchedAt: time.Now().UnixMilli()}
	setCachedQuota("c1", cq)
	got, ok := getCachedQuota("c1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.ConnectionID != "c1" {
		t.Errorf("connection id = %q", got.ConnectionID)
	}
	ClearQuotaCache("c1")
	_, ok = getCachedQuota("c1")
	if ok {
		t.Error("expected cache miss after clear")
	}
}

func TestQuotaCache_ExpiresAfterTTL(t *testing.T) {
	ClearQuotaCache("")
	cq := ConnectionQuota{ConnectionID: "c2"}
	cached := cachedQuota{cq: cq, fetchedAt: time.Now().Add(-31 * time.Second)}
	quotaCache.Store("c2", cached)
	_, ok := getCachedQuota("c2")
	if ok {
		t.Error("expected expired cache entry to miss")
	}
}

func TestIsEligibleForModel_NonCodexAlwaysEligible(t *testing.T) {
	quotas := []QuotaItem{{RemainingPct: 0, Scope: "codex"}}
	if !IsEligibleForModel("openai", "gpt-4o", quotas) {
		t.Error("non-codex should stay eligible")
	}
}

func TestIsEligibleForModel_CodexExhaustedBlocksCodexModel(t *testing.T) {
	quotas := []QuotaItem{{Name: "Session", RemainingPct: 0, Scope: "codex"}}
	if IsEligibleForModel("cx", "cx/gpt-5.4", quotas) {
		t.Error("expected codex model blocked when codex quota exhausted")
	}
}

func TestIsEligibleForModel_SparkExhaustedDoesNotBlockCodexModel(t *testing.T) {
	quotas := []QuotaItem{{Name: "Spark", RemainingPct: 0, Scope: "spark"}}
	if !IsEligibleForModel("cx", "cx/gpt-5.4", quotas) {
		t.Error("spark quota should not block non-spark model")
	}
}

func TestIsEligibleForModel_SparkExhaustedBlocksSparkModel(t *testing.T) {
	quotas := []QuotaItem{{Name: "Spark Session", RemainingPct: 0, Scope: "spark"}}
	if IsEligibleForModel("cx", "cx/gpt-5.3-codex-spark", quotas) {
		t.Error("expected spark model blocked when spark quota exhausted")
	}
}

func TestIsEligibleForModel_CodexExhaustedDoesNotBlockSparkModel(t *testing.T) {
	quotas := []QuotaItem{{Name: "Session", RemainingPct: 0, Scope: "codex"}}
	if !IsEligibleForModel("cx", "cx/gpt-5.3-codex-spark", quotas) {
		t.Error("codex quota should not block spark model")
	}
}

func TestIsEligibleForModel_DefaultScopeIsCodex(t *testing.T) {
	quotas := []QuotaItem{{Name: "Session", RemainingPct: 0, Scope: ""}}
	if IsEligibleForModel("cx", "cx/gpt-5.4", quotas) {
		t.Error("empty scope should default to codex")
	}
}
