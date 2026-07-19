package connstate

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestRecordFailure_ModelCooldown(t *testing.T) {
	store := NewStore()
	store.SeedConnection("conn-1", "openai", "ready", 0)

	until := time.Now().Add(time.Minute)
	det := ErrorDetection{
		Category:      ErrorRateLimit,
		Status:        StatusRateLimited,
		Scope:         "model",
		ModelID:       "gpt-4o",
		CooldownUntil: &until,
	}
	store.RecordFailure("conn-1", det)

	cs := store.Get("conn-1")
	if cs == nil {
		t.Fatal("connection state not found")
	}
	if !cs.IsModelInCooldown("gpt-4o") {
		t.Error("expected gpt-4o to be in cooldown")
	}
	if cs.IsModelInCooldown("gpt-3.5-turbo") {
		t.Error("did not expect gpt-3.5-turbo to be in cooldown")
	}
}

func TestRecordFailure_ConnectionCooldown(t *testing.T) {
	store := NewStore()
	store.SeedConnection("conn-1", "openai", "ready", 0)

	until := time.Now().Add(time.Minute)
	det := ErrorDetection{
		Category:      ErrorRateLimit,
		Status:        StatusRateLimited,
		Scope:         "connection",
		CooldownUntil: &until,
	}
	store.RecordFailure("conn-1", det)

	cs := store.Get("conn-1")
	if cs == nil {
		t.Fatal("connection state not found")
	}
	if !cs.IsInCooldown() {
		t.Error("expected connection to be in cooldown")
	}
}

func TestRecordFailure_QuotaCooldownSetsMidnight(t *testing.T) {
	store := NewStore()
	store.SeedConnection("conn-1", "cf", "ready", 0)

	det := ErrorDetection{
		Category: ErrorQuota,
		Status:   StatusQuotaExhausted,
		Scope:    "connection",
		// simulated: detector sets next midnight UTC + 1 min
		CooldownUntil: func() *time.Time { u := nextMidnightUTC().Add(time.Minute); return &u }(),
	}
	store.RecordFailure("conn-1", det)

	cs := store.Get("conn-1")
	if cs == nil {
		t.Fatal("connection state not found")
	}
	if cs.GetStatus() != StatusQuotaExhausted {
		t.Errorf("expected status quota_exhausted, got %s", cs.GetStatus())
	}
	if !cs.IsInCooldown() {
		t.Error("expected connection to be in cooldown")
	}
	if cs.BanCount != 1 {
		t.Errorf("expected ban count 1, got %d", cs.BanCount)
	}
}

func TestRecordFailure_PerModelKeepsConnectionReady(t *testing.T) {
	store := NewStore()
	store.SeedConnection("conn-1", "oc", "ready", 0)
	until := time.Now().Add(time.Minute)
	det := ErrorDetection{
		Category:      ErrorQuota,
		Status:        StatusQuotaExhausted,
		Scope:         "model",
		ModelID:       "oc/hy3-free",
		CooldownUntil: &until,
	}
	store.RecordFailure("conn-1", det)
	cs := store.Get("conn-1")
	if cs == nil {
		t.Fatal("connection state not found")
	}
	if !cs.IsModelInCooldown("oc/hy3-free") {
		t.Error("expected oc/hy3-free to be in cooldown")
	}
	if cs.GetStatus() != StatusReady {
		t.Errorf("expected connection status to stay ready, got %s", cs.GetStatus())
	}
	if cs.IsInCooldown() {
		t.Error("expected connection-level cooldown to be unset")
	}
}

func TestDetectError_429WithQuotaBody(t *testing.T) {
	// CF daily limit returns 429 with body containing quota patterns.
	// This must be classified as ErrorQuota, not ErrorRateLimit.
	cfBody := `{"errors":[{"message":"AiError: AiError: you have used up your daily free allocation of 10,000 neurons, please upgrade to Cloudflare's Workers Paid plan if you would like to continue usage. (de3fabb0-569c-4e72-bec9-fedd0de629b3)","code":4006}],"success":false,"result":{},"messages":[]}`

	det := DetectError(context.Background(),429, cfBody, nil, "cf", "", nil)
	if det.Category != ErrorQuota {
		t.Errorf("expected ErrorQuota for 429+neurons body, got %s", det.Category)
	}
	if det.Status != StatusQuotaExhausted {
		t.Errorf("expected StatusQuotaExhausted, got %s", det.Status)
	}
	if det.CooldownUntil == nil {
		t.Fatal("expected CooldownUntil to be set")
	}
	// Cooldown should be next midnight UTC + 1 min
	expected := nextMidnightUTC().Add(time.Minute)
	if det.CooldownUntil.Sub(expected).Abs() > time.Second {
		t.Errorf("expected cooldown ~%v, got %v", expected, det.CooldownUntil)
	}
}

func TestDetectError_429WithoutQuotaBody(t *testing.T) {
	// Regular rate limit (e.g. OpenAI) — 429 without quota patterns
	det := DetectError(context.Background(),429, `{"error":{"message":"rate limit exceeded"}}`, nil, "openai", "", nil)
	if det.Category != ErrorRateLimit {
		t.Errorf("expected ErrorRateLimit for plain 429, got %s", det.Category)
	}
}

func TestClassifyProviderUnavailable(t *testing.T) {
	tests := []struct {
		name   string
		states []Status
		want   ErrorCategory
	}{
		{"all quota", []Status{StatusQuotaExhausted, StatusQuotaExhausted}, ErrorQuota},
		{"all cooldown", []Status{StatusCooldown, StatusCooldown}, ErrorQuota},
		{"mixed quota+cooldown", []Status{StatusQuotaExhausted, StatusCooldown}, ErrorQuota},
		{"all auth", []Status{StatusAuthFailed, StatusAuthFailed}, ErrorAuth},
		{"all disabled", []Status{StatusDisabled, StatusDisabled}, ErrorBalanceEmpty},
		{"all balance empty", []Status{StatusBalanceEmpty, StatusBalanceEmpty}, ErrorBalanceEmpty},
		{"mixed quota+ready", []Status{StatusQuotaExhausted, StatusReady}, ErrorUnknown},
		{"empty provider", []Status{}, ErrorUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewStore()
			for i, st := range tt.states {
				store.SeedConnection(fmt.Sprintf("conn-%d", i), "grok-cli", "ready", i)
				cs := store.Get(fmt.Sprintf("conn-%d", i))
				cs.SetStatus(st, "")
			}
			if got := store.ClassifyProviderUnavailable("grok-cli"); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
