package connstate

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestDetectError_PerModelQuota_WithRetryAfter(t *testing.T) {
	body := `{"error":{"message":"Daily freeusage limit reached. Resets in 10 hours"}}`
	headers := http.Header{}
	headers.Set("Retry-After", "600")
	det := DetectError(context.Background(),429, body, nil, "oc", "oc/hy3-free", headers)

	if det.Category != ErrorQuota {
		t.Errorf("category=%v, want ErrorQuota", det.Category)
	}
	if det.Scope != "model" {
		t.Errorf("scope=%v, want model", det.Scope)
	}
	if det.CooldownUntil == nil {
		t.Fatal("expected CooldownUntil")
	}
	// Retry-After header should win over body text.
	want := time.Now().Add(600 * time.Second)
	if det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
	}
}

func TestDetectError_PerModelQuota_ResetsInBody(t *testing.T) {
	body := `{"error":{"message":"FreeUsageLimitError: daily quota exhausted. Resets in 16h"}}`
	det := DetectError(context.Background(),429, body, nil, "oc", "oc/deepseek-free", nil)

	if det.Category != ErrorQuota {
		t.Errorf("category=%v, want ErrorQuota", det.Category)
	}
	if det.Scope != "model" {
		t.Errorf("scope=%v, want model", det.Scope)
	}
	if det.CooldownUntil == nil {
		t.Fatal("expected CooldownUntil")
	}
	want := time.Now().Add(16 * time.Hour)
	if det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
	}
}

func TestDetectError_PerModelRateLimit_AGFamily(t *testing.T) {
	body := `{"error":{"message":"rate limit exceeded"}}`
	headers := http.Header{}
	headers.Set("Retry-After", "120")
	det := DetectError(context.Background(),429, body, nil, "ag", "ag/gemini-3-5", headers)

	if det.Scope != "model" {
		t.Errorf("scope=%v, want model", det.Scope)
	}
	if det.ModelID != "ag/gemini-3-5" {
		t.Errorf("modelID=%q, want ag/gemini-3-5", det.ModelID)
	}
	wantScope := "family:gemini"
	if got := ModelScope("ag", det.ModelID); got != wantScope {
		t.Errorf("ModelScope=%q, want %q", got, wantScope)
	}
	want := time.Now().Add(120 * time.Second)
	if det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
	}
}

func TestDetectError_NonPerModelRateLimit_KeepsConnectionScope(t *testing.T) {
	body := `{"error":{"message":"rate limit exceeded"}}`
	headers := http.Header{}
	headers.Set("Retry-After", "60")
	det := DetectError(context.Background(),429, body, nil, "openai", "gpt-4o", headers)

	if det.Scope != "connection" {
		t.Errorf("scope=%v, want connection", det.Scope)
	}
	want := time.Now().Add(60 * time.Second)
	if det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
	}
}

func TestModelScope_AntigravityFamilies(t *testing.T) {
	cases := []struct {
		model, want string
	}{
		{"gemini-3.1", "family:gemini"},
		{"ag/gemini-3.5", "family:gemini"},
		{"claude-sonnet-4-6", "family:claude"},
		{"ag/cloud-flash-5", "family:claude"},
		{"ag/anthropic-opus", "family:claude"},
		{"ag/mystery-model-9", "ag/mystery-model-9"},
	}
	for _, tc := range cases {
		if got := ModelScope("ag", tc.model); got != tc.want {
			t.Errorf("ModelScope(%q)=%q, want %q", tc.model, got, tc.want)
		}
	}
}

func TestDetectError_402InsufficientBalance_Disables(t *testing.T) {
	body := `{"error":{"message":"Insufficient Balance"}}`
	det := DetectError(context.Background(), http.StatusPaymentRequired, body, nil, "mimo", "mimo/payg", nil)

	if det.Category != ErrorBalanceEmpty {
		t.Errorf("category=%v, want ErrorBalanceEmpty", det.Category)
	}
	if det.Status != StatusDisabled {
		t.Errorf("status=%v, want StatusDisabled", det.Status)
	}
	if det.Retryable {
		t.Error("expected Retryable=false")
	}
	if det.CooldownUntil != nil {
		t.Errorf("expected no cooldown, got %v", det.CooldownUntil)
	}
}

func TestDetectError_GrokCLI_402SpendingLimit_IsQuota(t *testing.T) {
	body := `{"error":{"message":"spending-limit exceeded"}}`
	det := DetectError(context.Background(), http.StatusPaymentRequired, body, nil, "grok-cli", "grok-cli/grok-4.5", nil)

	if det.Category != ErrorQuota {
		t.Errorf("category=%v, want ErrorQuota", det.Category)
	}
	if det.Status != StatusQuotaExhausted {
		t.Errorf("status=%v, want StatusQuotaExhausted", det.Status)
	}
	if det.Scope != "connection" {
		t.Errorf("scope=%v, want connection", det.Scope)
	}
}

func TestDetectError_GrokCLI_402PersonalTeamBlocked_IsBalanceEmpty(t *testing.T) {
	body := `{"error":{"message":"personal-team-blocked"}}`
	det := DetectError(context.Background(), http.StatusPaymentRequired, body, nil, "grok-cli", "grok-cli/grok-4.5", nil)

	if det.Category != ErrorBalanceEmpty {
		t.Errorf("category=%v, want ErrorBalanceEmpty", det.Category)
	}
	if det.Status != StatusDisabled {
		t.Errorf("status=%v, want StatusDisabled", det.Status)
	}
	if det.Retryable {
		t.Error("expected Retryable=false")
	}
}

func TestHasPerModelQuota_GrokCLI_ReturnsFalse(t *testing.T) {
	if HasPerModelQuota("grok-cli") {
		t.Error("HasPerModelQuota(\"grok-cli\")=true, want false")
	}
}

func TestDetectError_ContextCanceled_IsTimeout(t *testing.T) {
	// A server-side cancellation (not the inbound request) must classify as a
	// retryable timeout, not ErrorUnknown.
	det := DetectError(context.Background(), 0, "", context.Canceled, "cf", "", nil)
	if det.Category != ErrorTimeout {
		t.Errorf("category=%v, want ErrorTimeout", det.Category)
	}
	if !det.Retryable {
		t.Error("expected Retryable=true")
	}
	if det.Status != StatusDegraded {
		t.Errorf("status=%v, want StatusDegraded", det.Status)
	}
}
