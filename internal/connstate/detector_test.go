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

func TestDetectError_FreeUsageExhausted429_IsQuotaWith24hCooldown(t *testing.T) {
	for _, body := range []string{
		`{"code":"free-usage-exhausted","error":"subscription changed"}`,
		`{"error":{"message":"You have included free usage of xAI API"}}`,
		`free-usage-exhausted`,
	} {
		det := DetectError(context.Background(), http.StatusTooManyRequests, body, nil, "xai", "xai/grok-3", nil)

		if det.Category != ErrorQuota {
			t.Errorf("body=%q: category=%v, want ErrorQuota", body, det.Category)
		}
		if det.Status != StatusQuotaExhausted {
			t.Errorf("body=%q: status=%v, want StatusQuotaExhausted", body, det.Status)
		}
		if det.CooldownUntil == nil {
			t.Fatalf("body=%q: expected CooldownUntil", body)
		}
		want := time.Now().Add(24 * time.Hour)
		if det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
			t.Errorf("body=%q: cooldown=%v, want around %v", body, det.CooldownUntil, want)
		}
	}
}

func TestDetectError_FreeUsageExhausted429_UsesRetryAfterHeader(t *testing.T) {
	body := `{"error":{"message":"free-usage-exhausted"}}`
	headers := http.Header{}
	headers.Set("Retry-After", "600")
	det := DetectError(context.Background(), http.StatusTooManyRequests, body, nil, "xai", "xai/grok-3", headers)

	if det.Category != ErrorQuota {
		t.Errorf("category=%v, want ErrorQuota", det.Category)
	}
	if det.Status != StatusQuotaExhausted {
		t.Errorf("status=%v, want StatusQuotaExhausted", det.Status)
	}
	if det.CooldownUntil == nil {
		t.Fatal("expected CooldownUntil")
	}
	want := time.Now().Add(600 * time.Second)
	if det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
	}
}

func TestDetectError_GrokCLI_403PermissionDenied_IsAuth(t *testing.T) {
	// Raw upstream body as reported by the user: error.message is a JSON string
	// that itself contains "permission-denied" and "insufficient_quota".
	body := `{"error":{"message":"{\"code\":\"permission-denied\",\"error\":\"Access to the chat endpoint is denied. Please ensure you're using the correct credentials. If you believe this is a mistake, please log into console.x.ai and update the permissions, or contact support.\",\"type\":\"permission_error\",\"code\":\"insufficient_quota\"}"}}`

	det := DetectError(context.Background(), http.StatusForbidden, body, nil, "grok-cli", "grok-cli/grok-4.5", nil)

	if det.Category != ErrorAuth {
		t.Errorf("category=%v, want ErrorAuth", det.Category)
	}
	if det.Status != StatusAuthFailed {
		t.Errorf("status=%v, want StatusAuthFailed", det.Status)
	}
	if det.CooldownUntil != nil {
		t.Errorf("expected no cooldown for auth error, got %v", det.CooldownUntil)
	}
}

func TestDetectError_GrokCLI_403PermissionDenied_Translated_IsAuth(t *testing.T) {
	// After the translator normalizes the body, the message still contains
	// "insufficient_quota" indirectly in the original upstream text. The
	// detector must still treat 403 as auth, never as quota/rate-limit.
	body := `{"error":{"message":"Access to the chat endpoint is denied. Please ensure you're using the correct credentials.","type":"permission_error","code":"permission_error"}}`

	det := DetectError(context.Background(), http.StatusForbidden, body, nil, "grok-cli", "grok-cli/grok-4.5", nil)

	if det.Category != ErrorAuth {
		t.Errorf("category=%v, want ErrorAuth", det.Category)
	}
	if det.Status != StatusAuthFailed {
		t.Errorf("status=%v, want StatusAuthFailed", det.Status)
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

func TestDetectError_CodeBuddy_CreditsExhausted14018_IsQuota(t *testing.T) {
	body := `{"error":{"message":"{\"error\":{\"data\":{\"code\":14018,\"msg\":\"Credits exhausted. Please visit the link below to purchase add-on packs and get more credits: https://www.codebuddy.ai/profile/usage\",\"requestId\":\"b8f02f06-8bb6-43bc-9f18-5289cbf91840\"}}}","type":"rate_limit_error","code":"rate_limit_exceeded"}}`

	det := DetectError(context.Background(), http.StatusTooManyRequests, body, nil, "codebuddy", "codebuddy/glm-5.2", nil)
	if det.Category != ErrorQuota {
		t.Errorf("category=%v, want ErrorQuota", det.Category)
	}
	if det.Status != StatusQuotaExhausted {
		t.Errorf("status=%v, want StatusQuotaExhausted", det.Status)
	}
	if det.Scope != "connection" {
		t.Errorf("scope=%v, want connection", det.Scope)
	}
	if det.CooldownUntil == nil {
		t.Fatal("expected CooldownUntil")
	}
}
