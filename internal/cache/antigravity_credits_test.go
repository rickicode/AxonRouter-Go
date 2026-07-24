package cache

import (
	"testing"
	"time"
)

func TestAntigravityCreditsBalanceCache_RoundTrip(t *testing.T) {
	ResetAntigravityCreditsCacheForTest()

	if _, ok := GetAntigravityCreditsBalance("auth-1"); ok {
		t.Fatal("expected no balance on empty cache")
	}

	want := AntigravityCreditsBalance{RemainingCredits: 123.45, ProbedAt: time.Now()}
	SetAntigravityCreditsBalance("auth-1", want)

	got, ok := GetAntigravityCreditsBalance("auth-1")
	if !ok {
		t.Fatal("expected cached balance")
	}
	if got.RemainingCredits != want.RemainingCredits {
		t.Errorf("RemainingCredits = %v, want %v", got.RemainingCredits, want.RemainingCredits)
	}
}

func TestAntigravityCreditsBalanceCache_Expires(t *testing.T) {
	ResetAntigravityCreditsCacheForTest()

	stale := AntigravityCreditsBalance{RemainingCredits: 1, ProbedAt: time.Now().Add(-10 * time.Minute)}
	SetAntigravityCreditsBalance("auth-expired", stale)

	if _, ok := GetAntigravityCreditsBalance("auth-expired"); ok {
		t.Fatal("expected stale balance to be treated as missing")
	}
}

func TestAntigravityCreditsBalanceCache_EmptyAuthIgnored(t *testing.T) {
	ResetAntigravityCreditsCacheForTest()

	SetAntigravityCreditsBalance("", AntigravityCreditsBalance{RemainingCredits: 1, ProbedAt: time.Now()})
	if _, ok := GetAntigravityCreditsBalance(""); ok {
		t.Fatal("expected empty authID to be ignored")
	}
}

func TestAntigravityCreditsPermanentlyDisabled(t *testing.T) {
	ResetAntigravityCreditsCacheForTest()

	if IsAntigravityCreditsPermanentlyDisabled("auth-broke") {
		t.Fatal("expected disabled=false before marking")
	}

	MarkAntigravityCreditsPermanentlyDisabled("auth-broke")
	if !IsAntigravityCreditsPermanentlyDisabled("auth-broke") {
		t.Fatal("expected disabled=true after marking")
	}

	if IsAntigravityCreditsPermanentlyDisabled("") {
		t.Fatal("expected empty authID to be ignored")
	}
}
