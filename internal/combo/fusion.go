package combo

import (
	"encoding/json"
	"fmt"
)

// FusionConfig holds fusion strategy configuration for a combo.
type FusionConfig struct {
	JudgeModel         string `json:"judge_model"`
	MinPanel           int    `json:"min_panel"`
	StragglerGraceMs   int    `json:"straggler_grace_ms"`
	PanelHardTimeoutMs int    `json:"panel_hard_timeout_ms"`
	AnonymizeSources   bool   `json:"anonymize_sources"`
}

// DefaultFusionConfig returns a FusionConfig with sensible defaults.
func DefaultFusionConfig() FusionConfig {
	return FusionConfig{
		MinPanel:           2,
		StragglerGraceMs:   8000,
		PanelHardTimeoutMs: 90000,
		AnonymizeSources:   true,
	}
}

// ParseFusionConfig parses a JSON string into a FusionConfig.
// Empty string returns defaults; malformed JSON returns an error.
func ParseFusionConfig(raw string) (FusionConfig, error) {
	cfg := DefaultFusionConfig()
	if raw == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return FusionConfig{}, fmt.Errorf("parse fusion_config: %w", err)
	}
	return cfg, nil
}

// SerializeFusionConfig serializes a FusionConfig to a JSON string.
func SerializeFusionConfig(cfg FusionConfig) (string, error) {
	b, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("serialize fusion_config: %w", err)
	}
	return string(b), nil
}

// Validate checks that the FusionConfig values are consistent with the
// number of steps in the combo. stepCount is the total number of steps.
// MinPanel is clamped to the number of steps so a single-step combo does not
// fail because of the default min_panel value.
func (cfg *FusionConfig) Validate(stepCount int) error {
	if stepCount == 0 {
		return fmt.Errorf("combo has no steps")
	}
	if cfg.MinPanel < 1 {
		cfg.MinPanel = 1
	}
	if cfg.MinPanel > stepCount {
		cfg.MinPanel = stepCount
	}
	if cfg.PanelHardTimeoutMs < 1000 {
		return fmt.Errorf("panel_hard_timeout_ms must be >= 1000, got %d", cfg.PanelHardTimeoutMs)
	}
	return nil
}
