package connstate

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// TestDetectError_CloudflareDailyQuota_ExhaustedUntilMidnight verifies that a
// Cloudflare free-tier daily neuron limit is treated as account-wide exhaustion
// and locked out until the next UTC midnight, not the generic 30-minute quota
// cooldown.
func TestDetectError_CloudflareDailyQuota_ExhaustedUntilMidnight(t *testing.T) {
	body := `{"errors":[{"message":"you have used up your daily free allocation of 10,000 neurons"}],"type":"invalid_request_error"}`
	det := DetectError(context.Background(), http.StatusTooManyRequests, body, nil, "cf", "cf/moonshotai/kimi-k2.7-code", nil)

	if det.Category != ErrorQuota {
		t.Errorf("category=%v, want ErrorQuota", det.Category)
	}
	if det.Status != StatusQuotaExhausted {
		t.Errorf("status=%v, want StatusQuotaExhausted", det.Status)
	}
	if det.CooldownUntil == nil {
		t.Fatal("expected CooldownUntil")
	}

	want := nextMidnightUTC().Add(time.Minute)
	if det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
	}
}

// TestDetectError_CloudflareDailyQuota_NestedJSON verifies detection works when
// the error message is wrapped inside Cloudflare's nested JSON format.
func TestDetectError_CloudflareDailyQuota_NestedJSON(t *testing.T) {
	body := `{"error":{"message":"{\"message\":\"you have used up your daily free allocation of 10,000 neurons\",\"type\":\"insufficient_quota\"}"}}`
	det := DetectError(context.Background(), http.StatusTooManyRequests, body, nil, "cf", "cf/model", nil)

	if det.Category != ErrorQuota {
		t.Errorf("category=%v, want ErrorQuota", det.Category)
	}
	if det.Status != StatusQuotaExhausted {
		t.Errorf("status=%v, want StatusQuotaExhausted", det.Status)
	}
	if det.CooldownUntil == nil {
		t.Fatal("expected CooldownUntil")
	}
}

// TestDetectError_NonCloudflareQuota_KeepsGenericCooldown verifies that other
// providers with insufficient_quota still use the generic 30-minute cooldown.
func TestDetectError_NonCloudflareQuota_KeepsGenericCooldown(t *testing.T) {
	body := `{"error":{"message":"insufficient_quota","type":"insufficient_quota"}}`
	det := DetectError(context.Background(), http.StatusTooManyRequests, body, nil, "openai", "openai/gpt-4o", nil)

	if det.Category != ErrorQuota {
		t.Errorf("category=%v, want ErrorQuota", det.Category)
	}
	if det.CooldownUntil == nil {
		t.Fatal("expected CooldownUntil")
	}

	want := time.Now().Add(quotaCooldown)
	if det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
	}
}
