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

func TestFusionConfigValidateRejectsNegativeStragglerGrace(t *testing.T) {
	cfg := DefaultFusionConfig()
	cfg.StragglerGraceMs = -1
	if err := cfg.Validate(2); err == nil {
		t.Fatalf("Validate should reject negative straggler_grace_ms")
	}
}

func TestFusionConfigValidateRejectsStragglerGraceGTOETimeout(t *testing.T) {
	cfg := DefaultFusionConfig()
	cfg.StragglerGraceMs = cfg.PanelHardTimeoutMs
	if err := cfg.Validate(2); err == nil {
		t.Fatalf("Validate should reject straggler_grace_ms >= panel_hard_timeout_ms")
	}

	cfg = DefaultFusionConfig()
	cfg.StragglerGraceMs = cfg.PanelHardTimeoutMs + 1
	if err := cfg.Validate(2); err == nil {
		t.Fatalf("Validate should reject straggler_grace_ms >= panel_hard_timeout_ms")
	}
}
