package quota

import (
	"strings"
	"testing"
)

func TestParseFreebuffQuota(t *testing.T) {
	body := `{
		"accessTier": "free",
		"rateLimitsByModel": {
			"deepseek/deepseek-v4-flash": {"limit": 6, "recentCount": 2, "resetAt": "2026-08-11T07:00:00Z"},
			"openai/gpt-5.6-luna": {"limit": 3, "recentCount": 3, "resetAt": "2026-08-11T07:00:00Z"}
		}
	}`
	quotas, plan, err := parseFreebuffQuota([]byte(body))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if plan != "Freebuff" {
		t.Errorf("plan=%q, want Freebuff", plan)
	}
	if len(quotas) != 2 {
		t.Fatalf("len(quotas)=%d, want 2", len(quotas))
	}
	byKey := map[string]QuotaItem{}
	for _, q := range quotas {
		byKey[q.ModelKey] = q
	}
	flash := byKey["deepseek/deepseek-v4-flash"]
	if flash.Name != "DeepSeek v4 Flash" {
		t.Errorf("flash name=%q, want DeepSeek v4 Flash", flash.Name)
	}
	if flash.Used != 2 || flash.Total != 6 {
		t.Errorf("flash used/total=%v/%v, want 2/6", flash.Used, flash.Total)
	}
	if flash.RemainingPct != 66.66666666666666 {
		t.Errorf("flash remaining=%v", flash.RemainingPct)
	}
	if flash.ResetAt != "2026-08-11T07:00:00Z" {
		t.Errorf("flash reset_at=%q", flash.ResetAt)
	}
	if flash.Unlimited {
		t.Errorf("flash should not be unlimited")
	}
	luna := byKey["openai/gpt-5.6-luna"]
	if luna.RemainingPct != 0 {
		t.Errorf("luna remaining=%v, want 0 (fully used)", luna.RemainingPct)
	}
}

func TestParseFreebuffQuota_LimitedPlan(t *testing.T) {
	body := `{"accessTier": "limited", "rateLimitsByModel": {}}`
	quotas, plan, err := parseFreebuffQuota([]byte(body))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if plan != "Freebuff (Limited)" {
		t.Errorf("plan=%q, want Freebuff (Limited)", plan)
	}
	if len(quotas) != 0 {
		t.Errorf("expected no quotas, got %d", len(quotas))
	}
}

func TestParseFreebuffQuota_FractionalRecentCount(t *testing.T) {
	// recentCount is fractional — a long agent run can consume 1.3 units.
	body := `{
		"accessTier": "free",
		"rateLimitsByModel": {
			"mimo/mimo-v2.5": {"limit": 6, "recentCount": 1.3, "resetAt": ""}
		}
	}`
	quotas, _, err := parseFreebuffQuota([]byte(body))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(quotas) != 1 {
		t.Fatalf("len(quotas)=%d, want 1", len(quotas))
	}
	if quotas[0].Used != 1.3 {
		t.Errorf("used=%v, want 1.3", quotas[0].Used)
	}
}

func TestParseFreebuffQuota_FoldsActiveSessionRateLimit(t *testing.T) {
	// Active session carries its own `rateLimit` row — fold it in when the
	// shared map omits the model (older servers).
	body := `{
		"accessTier": "free",
		"status": "active",
		"model": "deepseek/deepseek-v4-flash",
		"rateLimit": {"limit": 6, "recentCount": 4, "resetAt": "2026-08-11T07:00:00Z"},
		"rateLimitsByModel": {}
	}`
	quotas, _, err := parseFreebuffQuota([]byte(body))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(quotas) != 1 {
		t.Fatalf("len(quotas)=%d, want 1 (folded active session)", len(quotas))
	}
	if quotas[0].ModelKey != "deepseek/deepseek-v4-flash" || quotas[0].Used != 4 {
		t.Errorf("folded quota=%+v, want flash/used 4", quotas[0])
	}
}

func TestFreebuffForbiddenError(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{`{"status":"country_blocked"}`, "region"},
		{`{"status":"banned"}`, "banned"},
		{`{"status":"other","message":"denied"}`, "denied"},
		{`not json`, "403"},
	}
	for _, tc := range cases {
		err := freebuffForbiddenError([]byte(tc.body))
		if err == nil {
			t.Fatalf("expected error for %q", tc.body)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("error=%q, want contains %q", err.Error(), tc.want)
		}
	}
}

func TestFreebuffPlan(t *testing.T) {
	if got := freebuffPlan("limited"); got != "Freebuff (Limited)" {
		t.Errorf("freebuffPlan(limited)=%q", got)
	}
	if got := freebuffPlan("free"); got != "Freebuff" {
		t.Errorf("freebuffPlan(free)=%q", got)
	}
	if got := freebuffPlan(""); got != "Freebuff" {
		t.Errorf("freebuffPlan()=%q", got)
	}
}
