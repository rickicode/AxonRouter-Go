package compression

// Strategy ties the mode, lite config, and engine config together.
type Strategy struct {
	Mode    CompressionMode
	Lite    LiteConfig
	Caveman EngineConfig
	Rtk     EngineConfig
	Output  EngineConfig
}

// Apply runs the compression pipeline for the configured mode.
// Every sub-step is fail-open: on error the original body is returned.
func Apply(cfg Strategy, body []byte) ([]byte, EngineStats, error) {
	if cfg.Mode == ModeOff {
		return body, EngineStats{}, nil
	}

	// 1. Always-on lite (applies to every mode except off).
	liteBody, liteStats, _ := ApplyLite(body, cfg.Lite)

	// 2. Mode-specific engine.
	var engineStats EngineStats
	switch cfg.Mode {
	case ModeLite:
		// Lite-only
	case ModeStandard:
		if e, ok := Get("caveman"); ok {
			liteBody, engineStats, _ = e.Apply(liteBody, cfg.Caveman)
		}
	case ModeRtk:
		if e, ok := Get("rtk"); ok {
			liteBody, engineStats, _ = e.Apply(liteBody, cfg.Rtk)
		}
	case ModeAggressive, ModeUltra:
		// Phase 1: aggressive/ultra fall back to standard (caveman).
		if e, ok := Get("caveman"); ok {
			liteBody, engineStats, _ = e.Apply(liteBody, cfg.Caveman)
		}
	}

	// 3. Optional output-side compression (system-prompt injection).
	var outputStats EngineStats
	if cfg.Output != nil && boolValue(cfg.Output["enabled"]) {
		if e, ok := Get("output"); ok {
			liteBody, outputStats, _ = e.Apply(liteBody, cfg.Output)
		}
	}

	// Compute combined stats.
	totalOriginal := liteStats.OriginalTokens
	if totalOriginal == 0 {
		totalOriginal = EstimateTokens(string(body))
	}
	totalCompressed := EstimateTokens(string(liteBody))

	savings := 0.0
	if totalOriginal > 0 {
		savings = (1.0 - float64(totalCompressed)/float64(totalOriginal)) * 100
	}

	techniques := make([]string, len(liteStats.TechniquesUsed))
	copy(techniques, liteStats.TechniquesUsed)
	for _, t := range engineStats.TechniquesUsed {
		if !contains(techniques, t) {
			techniques = append(techniques, t)
		}
	}
	for _, t := range outputStats.TechniquesUsed {
		if !contains(techniques, t) {
			techniques = append(techniques, t)
		}
	}

	return liteBody, EngineStats{
		OriginalTokens:   totalOriginal,
		CompressedTokens: totalCompressed,
		SavingsPercent:   savings,
		TechniquesUsed:   techniques,
	}, nil
}

func boolValue(v interface{}) bool {
	if v == nil {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return b == "true" || b == "1" || b == "on"
	case int:
		return b != 0
	case int64:
		return b != 0
	default:
		return false
	}
}
