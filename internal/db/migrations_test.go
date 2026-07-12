package db

import "testing"

// TestValidateSeedPricing enforces the user's hard rule on the pricing seed:
// no duplicate model IDs and no $0 (free-tier) rows.
func TestValidateSeedPricing(t *testing.T) {
	// OK case mirrors the real seed shape (price > 0, unique IDs).
	ok := []struct {
		ID, Name                                 string
		In, Out, Reason, CachedRead, CachedWrite float64
	}{
		{"gpt-4o", "GPT-4o", 0.0025, 0.01, 0, 0, 0},
		{"claude-3-opus", "Claude 3 Opus", 0.015, 0.075, 0, 0, 0},
	}
	if err := validateSeedPricing(ok); err != nil {
		t.Fatalf("expected valid seed, got error: %v", err)
	}

	// Duplicate ID must be rejected.
	dup := append([]struct {
		ID, Name                                 string
		In, Out, Reason, CachedRead, CachedWrite float64
	}{}, ok...)
	dup = append(dup, struct {
		ID, Name                                 string
		In, Out, Reason, CachedRead, CachedWrite float64
	}{"gpt-4o", "GPT-4o dup", 0.0025, 0.01, 0, 0, 0})
	if err := validateSeedPricing(dup); err == nil {
		t.Fatal("expected duplicate model_id error, got nil")
	}

	// $0 input+output (free-tier without real price) must be rejected.
	zero := []struct {
		ID, Name                                 string
		In, Out, Reason, CachedRead, CachedWrite float64
	}{
		{"some-free-model", "Free Model", 0, 0, 0, 0, 0},
	}
	if err := validateSeedPricing(zero); err == nil {
		t.Fatal("expected $0 free-tier error, got nil")
	}
}
