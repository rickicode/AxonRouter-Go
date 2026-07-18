package quota

import (
	"encoding/json"
	"testing"
)

func TestParseGrokCliBilling(t *testing.T) {
	cases := []struct {
		name     string
		billing  string
		user     string
		wantPlan string
		wantLen  int
		checks   func(t *testing.T, quotas []QuotaItem)
	}{
		{
			name: "monthly included + on-demand",
			billing: `{
				"monthlyLimit": 1000,
				"includedUsed": 250,
				"onDemandCap": 500,
				"onDemandUsed": 100,
				"billingPeriodEnd": "2026-08-01T00:00:00Z"
			}`,
			user:     `{"subscriptionTier":"premium"}`,
			wantPlan: "Premium",
			wantLen:  2,
			checks: func(t *testing.T, q []QuotaItem) {
				if q[0].Name != "Monthly included" || q[0].Used != 250 || q[0].Total != 1000 {
					t.Fatalf("monthly quota mismatch: %+v", q[0])
				}
				if q[0].RemainingPct != 75 {
					t.Fatalf("expected 75%% remaining, got %f", q[0].RemainingPct)
				}
				if q[1].Name != "On-demand" || q[1].Used != 100 || q[1].Total != 500 {
					t.Fatalf("on-demand quota mismatch: %+v", q[1])
				}
			},
		},
		{
			name: "protobuf-json wrappers",
			billing: `{
				"config": {
					"monthlyLimit": {"val": 500},
					"includedUsed": {"val": 200},
					"prepaidBalance": {"val": 50}
				}
			}`,
			user:     `{}`,
			wantPlan: "Grok Build",
			wantLen:  2,
			checks: func(t *testing.T, q []QuotaItem) {
				if q[0].Total != 500 || q[0].Used != 200 {
					t.Fatalf("wrapped monthly quota mismatch: %+v", q[0])
				}
				if q[1].Name != "Prepaid" || q[1].Total != 50 {
					t.Fatalf("prepaid quota mismatch: %+v", q[1])
				}
			},
		},
	{
		name: "free account without paid quota",
		billing: `{
			"onDemandCap": 0,
			"onDemandUsed": 0
		}`,
		user:     `{}`,
		wantPlan: "Grok Build",
		wantLen:  0,
	},
		{
			name:     "paid subscription fallback unlimited",
			billing:  `{}`,
			user:     `{"subscriptionTier":"pro"}`,
			wantPlan: "Pro",
			wantLen:  1,
			checks: func(t *testing.T, q []QuotaItem) {
				if !q[0].Unlimited {
					t.Fatalf("expected unlimited quota, got %+v", q[0])
				}
			},
		},
		{
			name:     "grok code plan",
			billing:  `{}`,
			user:     `{"hasGrokCodeAccess":true}`,
			wantPlan: "Grok Code",
			wantLen:  0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var billing, user map[string]any
			if err := json.Unmarshal([]byte(tc.billing), &billing); err != nil {
				t.Fatalf("billing unmarshal: %v", err)
			}
			if err := json.Unmarshal([]byte(tc.user), &user); err != nil {
				t.Fatalf("user unmarshal: %v", err)
			}
			plan, quotas := parseGrokCliBilling(billing, user)
			if plan != tc.wantPlan {
				t.Fatalf("plan=%q, want %q", plan, tc.wantPlan)
			}
			if len(quotas) != tc.wantLen {
				t.Fatalf("len(quotas)=%d, want %d", len(quotas), tc.wantLen)
			}
			if tc.checks != nil {
				tc.checks(t, quotas)
			}
		})
	}
}

func TestGrokCliTitleCase(t *testing.T) {
	if got := grokCliTitleCase("super_user-lite"); got != "Super User Lite" {
		t.Fatalf("got %q", got)
	}
}

func TestGrokCliParseResetTime(t *testing.T) {
	if got := parseGrokCliResetTime("2026-07-18T12:00:00Z"); got == "" {
		t.Fatal("expected parsed RFC3339")
	}
	if got := parseGrokCliResetTime("invalid", ""); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
