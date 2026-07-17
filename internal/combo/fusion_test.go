package combo

import "testing"

func TestFusionConfigValidateClampsMinPanel(t *testing.T) {
	cfg := DefaultFusionConfig()
	if err := cfg.Validate(1); err != nil {
		t.Fatalf("Validate(1) failed: %v", err)
	}
	if cfg.MinPanel != 1 {
		t.Fatalf("MinPanel clamped to %d, want 1", cfg.MinPanel)
	}
}

func TestFusionConfigValidateRejectsZeroSteps(t *testing.T) {
	cfg := DefaultFusionConfig()
	if err := cfg.Validate(0); err == nil {
		t.Fatalf("Validate(0) should reject combos with no steps")
	}
}
