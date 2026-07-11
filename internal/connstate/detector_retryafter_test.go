package connstate

import (
	"testing"
	"time"

	"github.com/rickicode/AxonRouter-Go/internal/executor"
)

func TestDetectError_UsesRetryAfterHeader(t *testing.T) {
	upErr := &executor.UpstreamError{
		StatusCode: 429,
		Body:       []byte(`{"error":{"message":"rate limited"}}`),
		RawBody:    []byte(`{"error":{"message":"rate limited"}}`),
	}
	upErr.Headers = make(map[string][]string)
	upErr.Headers.Set("Retry-After", "120")

	det := DetectError(0, "", upErr, "openai", "gpt-4o", nil)
	if det.Category != ErrorRateLimit {
		t.Errorf("category=%v, want ErrorRateLimit", det.Category)
	}
	if det.CooldownUntil == nil {
		t.Fatal("expected cooldown")
	}
	want := time.Now().Add(120 * time.Second)
	if det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
	}
}

func TestDetectError_UsesRetryAfterFromBody(t *testing.T) {
	upErr := &executor.UpstreamError{
		StatusCode: 429,
		Body:       []byte(`{"error":{"message":"rate limited","retry_after":90}}`),
		RawBody:    []byte(`{"error":{"message":"rate limited","retry_after":90}}`),
	}
	det := DetectError(0, "", upErr, "openai", "gpt-4o", nil)
	if det.CooldownUntil == nil {
		t.Fatal("expected cooldown")
	}
	want := time.Now().Add(90 * time.Second)
	if det.CooldownUntil.Before(want.Add(-2*time.Second)) || det.CooldownUntil.After(want.Add(2*time.Second)) {
		t.Errorf("cooldown=%v, want around %v", det.CooldownUntil, want)
	}
}
