package middleware

import (
	"testing"
	"time"
)

func TestRateLimitRefill(t *testing.T) {
	rl := NewRateLimiter(30)

	// Exhaust the bucket.
	for i := 0; i < 30; i++ {
		if !rl.Allow("test-key", 30) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	if rl.Allow("test-key", 30) {
		t.Fatal("request over limit should be denied")
	}

	// Wait long enough to earn one token at 30 req/min.
	time.Sleep(2 * time.Second)

	if !rl.Allow("test-key", 30) {
		t.Fatal("expected 1 additional token after 2 seconds for 30 req/min limit")
	}

	if rl.Allow("test-key", 30) {
		t.Fatal("only one refill token should be available")
	}
}
