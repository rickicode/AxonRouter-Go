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
	det := DetectError(context.Background(), 429, body, nil, "oc", "oc/hy3-free", headers)

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
	det := DetectError(context.Background(), 429, body, nil, "oc", "oc/deepseek-free", nil)

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
	det := DetectError(context.Background(), 429, body, nil, "ag", "ag/gemini-3-5", headers)

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
	det := DetectError(context.Background(), 429, body, nil, "openai", "gpt-4o", headers)

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
	if det.DisabledReason != "balance_empty" {
		t.Errorf("disabledReason=%q, want balance_empty", det.DisabledReason)
	}
	if det.Retryable {
		t.Error("expected Retryable=false")
	}
	if det.CooldownUntil == nil {
		t.Fatal("expected cooldown")
	}
	want := time.Now().Add(30 * time.Minute)
	if det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
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
	if det.DisabledReason != "balance_empty" {
		t.Errorf("disabledReason=%q, want balance_empty", det.DisabledReason)
	}
	if det.Retryable {
		t.Error("expected Retryable=false")
	}
	if det.CooldownUntil == nil {
		t.Fatal("expected cooldown")
	}
	want := time.Now().Add(30 * time.Minute)
	if det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
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
	body := `{"error":{"message":"\"{\\"code\\":\\"permission-denied\\",\\"error\\":\\"Access to the chat endpoint is denied. Please ensure you're using the correct credentials. If you believe this is a mistake, please log into console.x.ai and update the permissions, or contact support.\\",\\"type\\":\\"permission_error\\",\\"code\\":\\"insufficient_quota\\"}\""}}`
	det := DetectError(context.Background(), http.StatusForbidden, body, nil, "grok-cli", "grok-cli/grok-4.5", nil)
	if det.Category != ErrorAuth {
		t.Errorf("category=%v, want ErrorAuth", det.Category)
	}
	if det.Status != StatusDisabled {
		t.Errorf("status=%v, want StatusDisabled", det.Status)
	}
	if det.DisabledReason != "auth_failed" {
		t.Errorf("disabledReason=%q, want auth_failed", det.DisabledReason)
	}
	if det.CooldownUntil == nil {
		t.Fatal("expected cooldown")
	}
	want := time.Now().Add(30 * time.Minute)
	if det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
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
	if det.Status != StatusDisabled {
		t.Errorf("status=%v, want StatusDisabled", det.Status)
	}
	if det.DisabledReason != "auth_failed" {
		t.Errorf("disabledReason=%q, want auth_failed", det.DisabledReason)
	}
	if det.CooldownUntil == nil {
		t.Fatal("expected cooldown")
	}
	want := time.Now().Add(30 * time.Minute)
	if det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
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

func TestClassifyFromResponse_GlobalStatusCodeCategories(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   ErrorCategory
	}{
		{401, "", ErrorAuth},
		{403, "some quota text", ErrorAuth},
		{408, "", ErrorTimeout},
		{500, "", ErrorServer},
		{503, "rate limit", ErrorServer},
	}
	d := NewDetector()
	for _, tc := range cases {
		got := d.ClassifyFromResponse(tc.status, tc.body)
		if got != tc.want {
			t.Errorf("status=%d body=%q: got %v, want %v", tc.status, tc.body, got, tc.want)
		}
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

func TestDetectError_InsufficientQuota_30mCooldown(t *testing.T) {
	bodies := []string{
		`{"error":{"code":"insufficient_quota","message":"You exceeded your current quota"}}`,
		`{"error":{"type":"insufficient_quota"}}`,
		`insufficient_quota`,
	}
	for _, body := range bodies {
		det := DetectError(context.Background(), http.StatusTooManyRequests, body, nil, "openai", "gpt-5", nil)
		if det.Category != ErrorQuota {
			t.Errorf("body=%q: category=%v, want ErrorQuota", body, det.Category)
		}
		if det.Status != StatusQuotaExhausted {
			t.Errorf("body=%q: status=%v, want StatusQuotaExhausted", body, det.Status)
		}
		if det.CooldownUntil == nil {
			t.Fatalf("body=%q: expected CooldownUntil", body)
		}
		want := time.Now().Add(30 * time.Minute)
		if det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
			t.Errorf("body=%q: cooldown=%v, want around %v", body, det.CooldownUntil, want)
		}
	}
}

func TestDetectError_BillingHardLimitReached_30mCooldown(t *testing.T) {
	body := `{"error":{"code":"billing_hard_limit_reached"}}`
	det := DetectError(context.Background(), 429, body, nil, "openai", "gpt-5", nil)
	if det.Category != ErrorQuota {
		t.Errorf("category=%v, want ErrorQuota", det.Category)
	}
	if det.Status != StatusQuotaExhausted {
		t.Errorf("status=%v, want StatusQuotaExhausted", det.Status)
	}
	want := time.Now().Add(30 * time.Minute)
	if det.CooldownUntil == nil || det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
	}
}

func TestDetectError_RateLimitExponentialBackoff(t *testing.T) {
	resetRateLimitBackoffForTest()
	prev := rateLimitJitter
	rateLimitJitter = func(d time.Duration) time.Duration { return d }
	defer func() { rateLimitJitter = prev }()

	body := `{"error":{"code":"rate_limit_exceeded"}}`
	det1 := DetectError(context.Background(), 429, body, nil, "openai", "gpt-4o", nil)
	if det1.Category != ErrorRateLimit {
		t.Errorf("category=%v, want ErrorRateLimit", det1.Category)
	}
	if det1.Status != StatusRateLimited {
		t.Errorf("status=%v, want StatusRateLimited", det1.Status)
	}
	want1 := time.Now().Add(1 * time.Second)
	if det1.CooldownUntil.Before(want1.Add(-2*time.Second)) || det1.CooldownUntil.After(want1.Add(2*time.Second)) {
		t.Errorf("first cooldown=%v, want around %v", det1.CooldownUntil, want1)
	}

	det2 := DetectError(context.Background(), 429, body, nil, "openai", "gpt-4o", nil)
	want2 := time.Now().Add(2 * time.Second)
	if det2.CooldownUntil.Before(want2.Add(-2*time.Second)) || det2.CooldownUntil.After(want2.Add(2*time.Second)) {
		t.Errorf("second cooldown=%v, want around %v", det2.CooldownUntil, want2)
	}
}

func TestDetectError_RateLimit_RetryAfterHeaderWithJitter(t *testing.T) {
	prev := rateLimitJitter
	rateLimitJitter = func(d time.Duration) time.Duration { return d }
	defer func() { rateLimitJitter = prev }()

	body := `{"error":{"code":"rate_limit_exceeded"}}`
	headers := http.Header{}
	headers.Set("Retry-After", "120")
	det := DetectError(context.Background(), 429, body, nil, "openai", "gpt-4o", headers)
	if det.Category != ErrorRateLimit {
		t.Errorf("category=%v, want ErrorRateLimit", det.Category)
	}
	want := time.Now().Add(120 * time.Second)
	if det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
	}
}

func TestDetectError_RateLimit_RetryAfterBody(t *testing.T) {
	prev := rateLimitJitter
	rateLimitJitter = func(d time.Duration) time.Duration { return d }
	defer func() { rateLimitJitter = prev }()

	body := `{"error":{"message":"rate limit exceeded","retry_after":90}}`
	det := DetectError(context.Background(), 429, body, nil, "openai", "gpt-4o", nil)
	if det.Category != ErrorRateLimit {
		t.Errorf("category=%v, want ErrorRateLimit", det.Category)
	}
	want := time.Now().Add(90 * time.Second)
	if det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
	}
}

func TestDetectError_InvalidAPIKey(t *testing.T) {
	body := `{"error":{"code":"invalid_api_key","message":"Invalid API key"}}`
	det := DetectError(context.Background(), http.StatusUnauthorized, body, nil, "openai", "gpt-4o", nil)
	if det.Category != ErrorAuth {
		t.Errorf("category=%v, want ErrorAuth", det.Category)
	}
	if det.Status != StatusDisabled {
		t.Errorf("status=%v, want StatusDisabled", det.Status)
	}
	if det.DisabledReason != "auth_failed" {
		t.Errorf("disabledReason=%q, want auth_failed", det.DisabledReason)
	}
	want := time.Now().Add(30 * time.Minute)
	if det.CooldownUntil == nil || det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
	}
}

func TestDetectError_IncorrectAPIKey(t *testing.T) {
	body := `{"error":{"type":"incorrect_api_key"}}`
	det := DetectError(context.Background(), 401, body, nil, "openai", "gpt-4o", nil)
	if det.Category != ErrorAuth {
		t.Errorf("category=%v, want ErrorAuth", det.Category)
	}
	if det.Status != StatusDisabled {
		t.Errorf("status=%v, want StatusDisabled", det.Status)
	}
	if det.DisabledReason != "auth_failed" {
		t.Errorf("disabledReason=%q, want auth_failed", det.DisabledReason)
	}
}

func TestDetectError_AuthFailed(t *testing.T) {
	body := `{"error":{"code":"auth_failed","message":"Authentication failed"}}`
	det := DetectError(context.Background(), 403, body, nil, "openai", "gpt-4o", nil)
	if det.Category != ErrorAuth {
		t.Errorf("category=%v, want ErrorAuth", det.Category)
	}
	if det.Status != StatusDisabled {
		t.Errorf("status=%v, want StatusDisabled", det.Status)
	}
	if det.DisabledReason != "auth_failed" {
		t.Errorf("disabledReason=%q, want auth_failed", det.DisabledReason)
	}
}

func TestDetectError_ModelNotFound_ScopedCooldown(t *testing.T) {
	body := `{"error":{"code":"model_not_found","message":"The model 'gpt-99' does not exist"}}`
	det := DetectError(context.Background(), 404, body, nil, "openai", "gpt-99", nil)
	if det.Category != ErrorModelNotFound {
		t.Errorf("category=%v, want ErrorModelNotFound", det.Category)
	}
	if det.Scope != "model" {
		t.Errorf("scope=%v, want model", det.Scope)
	}
	want := time.Now().Add(12 * time.Hour)
	if det.CooldownUntil == nil || det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
	}
}

func TestDetectError_ModelNotSupported(t *testing.T) {
	body := `{"error":{"code":"model_not_supported"}}`
	det := DetectError(context.Background(), http.StatusBadRequest, body, nil, "openai", "o1", nil)
	if det.Category != ErrorModelNotFound {
		t.Errorf("category=%v, want ErrorModelNotFound", det.Category)
	}
	if det.Scope != "model" {
		t.Errorf("scope=%v, want model", det.Scope)
	}
}

func TestDetectError_Upstream5xx_Cooldown(t *testing.T) {
	body := `{"error":{"message":"Internal server error"}}`
	det := DetectError(context.Background(), http.StatusServiceUnavailable, body, nil, "openai", "gpt-4o", nil)
	if det.Category != ErrorServer {
		t.Errorf("category=%v, want ErrorServer", det.Category)
	}
	if det.Status != StatusDegraded {
		t.Errorf("status=%v, want StatusDegraded", det.Status)
	}
	if !det.Retryable {
		t.Error("expected Retryable=true")
	}
	want := time.Now().Add(1 * time.Minute)
	if det.CooldownUntil == nil || det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
	}
}

func TestDetectError_ContextCanceled_Cooldown(t *testing.T) {
	det := DetectError(context.Background(), 0, "", context.Canceled, "openai", "gpt-4o", nil)
	if det.Category != ErrorTimeout {
		t.Errorf("category=%v, want ErrorTimeout", det.Category)
	}
	if det.Status != StatusDegraded {
		t.Errorf("status=%v, want StatusDegraded", det.Status)
	}
	if !det.Retryable {
		t.Error("expected Retryable=true")
	}
	want := time.Now().Add(1 * time.Minute)
	if det.CooldownUntil == nil || det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
	}
}

func TestDetectError_StructuredJSONInsufficientQuota(t *testing.T) {
	// Only structured JSON fields; no human message text to match against.
	body := `{"code":"insufficient_quota"}`
	det := DetectError(context.Background(), 429, body, nil, "openai", "gpt-4o", nil)
	if det.Category != ErrorQuota {
		t.Errorf("category=%v, want ErrorQuota", det.Category)
	}
	if det.Status != StatusQuotaExhausted {
		t.Errorf("status=%v, want StatusQuotaExhausted", det.Status)
	}
	want := time.Now().Add(30 * time.Minute)
	if det.CooldownUntil == nil || det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
	}
}
