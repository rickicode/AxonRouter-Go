package compression

// Strategy ties the mode, lite config, and engine config together.
type Strategy struct {
	Mode    CompressionMode
	Lite    LiteConfig
	Caveman EngineConfig
	Rtk     EngineConfig
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
		found := false
		for _, existing := range techniques {
			if existing == t {
				found = true
				break
			}
		}
		if !found {
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
