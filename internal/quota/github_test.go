package quota

import (
	"testing"
)

func TestParseCopilotQuotas_PaidSnapshots(t *testing.T) {
	data := map[string]any{
		"copilot_plan":    "Copilot Pro",
		"quota_reset_date": "2026-08-01T00:00:00Z",
		"quota_snapshots": map[string]any{
			"chat": map[string]any{
				"entitlement": 1000,
				"remaining":   750,
				"unlimited":   false,
			},
			"completions": map[string]any{
				"entitlement": 5000,
				"remaining":   0,
				"unlimited":   false,
			},
			"premium_interactions": map[string]any{
				"entitlement": 100,
				"remaining":   100,
				"unlimited":   true,
			},
		},
	}

	quotas, plan, err := parseCopilotQuotas(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan != "Copilot Pro" {
		t.Errorf("plan = %q, want Copilot Pro", plan)
	}
	if len(quotas) != 3 {
		t.Fatalf("got %d quotas, want 3", len(quotas))
	}

	find := func(name string) *QuotaItem {
		for i := range quotas {
			if quotas[i].Name == name {
				return &quotas[i]
			}
		}
		return nil
	}

	chat := find("chat")
	if chat == nil {
		t.Fatal("missing chat quota")
	}
	if chat.Used != 250 || chat.Total != 1000 {
		t.Errorf("chat used=%v total=%v, want 250/1000", chat.Used, chat.Total)
	}
	if chat.RemainingPct != 75 {
		t.Errorf("chat remaining %% = %v, want 75", chat.RemainingPct)
	}

	comp := find("completions")
	if comp == nil {
		t.Fatal("missing completions quota")
	}
	if comp.RemainingPct != 0 {
		t.Errorf("completions remaining %% = %v, want 0", comp.RemainingPct)
	}

	prem := find("premium_interactions")
	if prem == nil {
		t.Fatal("missing premium_interactions quota")
	}
	if !prem.Unlimited {
		t.Error("premium_interactions should be unlimited")
	}
}

func TestParseCopilotQuotas_FreeLimited(t *testing.T) {
	data := map[string]any{
		"copilot_plan":          "Copilot Free",
		"limited_user_reset_date": "2026-08-01T00:00:00Z",
		"monthly_quotas": map[string]any{
			"chat":        100,
			"completions": 2000,
		},
		"limited_user_quotas": map[string]any{
			"chat":        25,
			"completions": 1000,
		},
	}

	quotas, plan, err := parseCopilotQuotas(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan != "Copilot Free" {
		t.Errorf("plan = %q, want Copilot Free", plan)
	}
	if len(quotas) != 2 {
		t.Fatalf("got %d quotas, want 2", len(quotas))
	}

	find := func(name string) *QuotaItem {
		for i := range quotas {
			if quotas[i].Name == name {
				return &quotas[i]
			}
		}
		return nil
	}

	chat := find("chat")
	if chat == nil {
		t.Fatal("missing chat quota")
	}
	if chat.Used != 25 || chat.Total != 100 || chat.RemainingPct != 75 {
		t.Errorf("chat = %+v, want used=25 total=100 remainingPct=75", chat)
	}
}
