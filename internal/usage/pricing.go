package usage

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"sync"
	"time"
)

type Pricing struct {
	InputPer1K             float64
	OutputPer1K            float64
	ReasonPer1K            float64
	ImagePerUnit           float64
	AudioPerMin            float64
	CachedReadPer1K        float64
	CachedWritePer1K       float64
	TierFlexMultiplier     float64
	TierPriorityMultiplier float64
	TierFastMultiplier     float64
}

var defaultPricing = Pricing{InputPer1K: 0.001, OutputPer1K: 0.002, TierFlexMultiplier: 0.5, TierPriorityMultiplier: 1.5, TierFastMultiplier: 2.5}

type ModelPricingRow struct {
	ModelID                string  `json:"model_id"`
	DisplayName            string  `json:"display_name"`
	InputPer1K             float64 `json:"input_per_1k"`
	OutputPer1K            float64 `json:"output_per_1k"`
	ReasonPer1K            float64 `json:"reason_per_1k"`
	CachedReadPer1K        float64 `json:"cached_read_per_1k"`
	CachedWritePer1K       float64 `json:"cached_write_per_1k"`
	ImagePerUnit           float64 `json:"image_per_unit"`
	AudioPerMin            float64 `json:"audio_per_min"`
	Currency               string  `json:"currency"`
	TierFlexMultiplier     float64 `json:"tier_flex_multiplier"`
	TierPriorityMultiplier float64 `json:"tier_priority_multiplier"`
	TierFastMultiplier     float64 `json:"tier_fast_multiplier"`
	UpdatedAt              int64   `json:"updated_at"`
	// ServiceKinds is populated from the model catalog for display; not persisted.
	ServiceKinds []string `json:"service_kinds,omitempty"`
}

var (
	pricingDB   *sql.DB
	pricingMu   sync.RWMutex
	pricingRows = map[string]ModelPricingRow{}
)

// InitPricing loads pricing from DB.
func InitPricing(db *sql.DB) {
	pricingDB = db
	ReloadPricing()
}

// ReloadPricing refreshes the in-memory cache from the DB.
func ReloadPricing() {
	if pricingDB == nil {
		return
	}
	rows, err := pricingDB.Query(`SELECT model_id, display_name, input_per_1k, output_per_1k, reason_per_1k, cached_read_per_1k, cached_write_per_1k, image_per_unit, audio_per_min, currency, tier_flex_multiplier, tier_priority_multiplier, tier_fast_multiplier, updated_at FROM model_pricing`)
	if err != nil {
		return
	}
	defer rows.Close()
	fresh := map[string]ModelPricingRow{}
	for rows.Next() {
		var r ModelPricingRow
		if err := rows.Scan(
			&r.ModelID, &r.DisplayName, &r.InputPer1K, &r.OutputPer1K, &r.ReasonPer1K,
			&r.CachedReadPer1K, &r.CachedWritePer1K, &r.ImagePerUnit, &r.AudioPerMin,
			&r.Currency, &r.TierFlexMultiplier, &r.TierPriorityMultiplier, &r.TierFastMultiplier, &r.UpdatedAt,
		); err != nil {
			continue
		}
		fresh[r.ModelID] = r
	}
	pricingMu.Lock()
	pricingRows = fresh
	pricingMu.Unlock()
}

// splitModel splits a model id after the first slash.
// StartPeriodicReload refreshes the pricing cache at the given interval.
func StartPeriodicReload(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				ReloadPricing()
			case <-ctx.Done():
				return
			}
		}
	}()
}

func defaultIfZero(v, fallback float64) float64 {
	if v == 0 {
		return fallback
	}
	return v
}

func splitModel(s string) (string, string) {
	for i, c := range s {
		if c == '/' {
			prefix := strings.TrimPrefix(s[:i], "@")
			return prefix, s[i+1:]
		}
	}
	return "", s
}

// GetPricing returns the pricing for a model. Provider prefixes are stripped.
func GetPricing(modelID string) Pricing {
	_, model := splitModel(modelID)

	pricingMu.RLock()
	rows := pricingRows
	pricingMu.RUnlock()

	if r, ok := rows[model]; ok {
		return Pricing{
			InputPer1K:             r.InputPer1K,
			OutputPer1K:            r.OutputPer1K,
			ReasonPer1K:            r.ReasonPer1K,
			ImagePerUnit:           r.ImagePerUnit,
			AudioPerMin:            r.AudioPerMin,
			CachedReadPer1K:        r.CachedReadPer1K,
			CachedWritePer1K:       r.CachedWritePer1K,
			TierFlexMultiplier:     defaultIfZero(r.TierFlexMultiplier, defaultPricing.TierFlexMultiplier),
			TierPriorityMultiplier: defaultIfZero(r.TierPriorityMultiplier, defaultPricing.TierPriorityMultiplier),
			TierFastMultiplier:     defaultIfZero(r.TierFastMultiplier, defaultPricing.TierFastMultiplier),
		}
	}

	// Deterministic longest-substring fallback.
	var matches []string
	for key := range rows {
		if strings.Contains(model, key) {
			matches = append(matches, key)
		}
	}
	if len(matches) > 0 {
		sort.Slice(matches, func(i, j int) bool {
			if len(matches[i]) != len(matches[j]) {
				return len(matches[i]) > len(matches[j])
			}
			return matches[i] < matches[j]
		})
		r := rows[matches[0]]
		return Pricing{
			InputPer1K:             r.InputPer1K,
			OutputPer1K:            r.OutputPer1K,
			ReasonPer1K:            r.ReasonPer1K,
			ImagePerUnit:           r.ImagePerUnit,
			AudioPerMin:            r.AudioPerMin,
			CachedReadPer1K:        r.CachedReadPer1K,
			CachedWritePer1K:       r.CachedWritePer1K,
			TierFlexMultiplier:     defaultIfZero(r.TierFlexMultiplier, defaultPricing.TierFlexMultiplier),
			TierPriorityMultiplier: defaultIfZero(r.TierPriorityMultiplier, defaultPricing.TierPriorityMultiplier),
			TierFastMultiplier:     defaultIfZero(r.TierFastMultiplier, defaultPricing.TierFastMultiplier),
		}
	}
	return defaultPricing
}

// EstimateCost returns the estimated cost in USD using the standard service tier.
// Token convention: inputTokens is cache-inclusive (input + cache_read + cache_creation),
// matching the per-provider usage reports after extraction. Cache-read tokens are billed at the
// read rate and cache-creation tokens at the write rate (falling back to the input rate when no
// dedicated write rate is configured). This mirrors OmniRoute's computeCostFromPricing.
func EstimateCost(modelID string, inputTokens, outputTokens, reasoningTokens, cachedTokens, cacheCreationTokens int64) float64 {
	return EstimateCostWithServiceTier(modelID, "", inputTokens, outputTokens, reasoningTokens, cachedTokens, cacheCreationTokens)
}

// EstimateCostWithServiceTier returns the estimated cost in USD with a tier multiplier applied.
// Supported tiers: flex (0.5x), priority (1.5x), fast (2.5x). Empty/unknown tiers default to
// standard (1.0x). Multipliers are configurable per model via model_pricing.
func EstimateCostWithServiceTier(modelID, serviceTier string, inputTokens, outputTokens, reasoningTokens, cachedTokens, cacheCreationTokens int64) float64 {
	p := GetPricing(modelID)
	nonCached := inputTokens - cachedTokens - cacheCreationTokens
	if nonCached < 0 {
		nonCached = 0
	}
	writeRate := p.CachedWritePer1K
	if writeRate == 0 {
		writeRate = p.InputPer1K
	}
	cost := float64(nonCached) / 1000.0 * p.InputPer1K
	cost += float64(cachedTokens) / 1000.0 * p.CachedReadPer1K
	cost += float64(cacheCreationTokens) / 1000.0 * writeRate
	cost += float64(outputTokens) / 1000.0 * p.OutputPer1K
	cost += float64(reasoningTokens) / 1000.0 * p.ReasonPer1K

	multiplier := 1.0
	switch strings.ToLower(serviceTier) {
	case "flex":
		multiplier = p.TierFlexMultiplier
	case "priority":
		multiplier = p.TierPriorityMultiplier
	case "fast":
		multiplier = p.TierFastMultiplier
	}
	if multiplier == 0 {
		multiplier = 1.0
	}
	return cost * multiplier
}

// ListPricing returns all rows from DB.
func ListPricing() []ModelPricingRow {
	if pricingDB == nil {
		return nil
	}
	rows, err := pricingDB.Query(`SELECT model_id, display_name, input_per_1k, output_per_1k, reason_per_1k, cached_read_per_1k, cached_write_per_1k, image_per_unit, audio_per_min, currency, tier_flex_multiplier, tier_priority_multiplier, tier_fast_multiplier, updated_at FROM model_pricing ORDER BY model_id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []ModelPricingRow{}
	for rows.Next() {
		var r ModelPricingRow
		if err := rows.Scan(
			&r.ModelID, &r.DisplayName, &r.InputPer1K, &r.OutputPer1K, &r.ReasonPer1K,
			&r.CachedReadPer1K, &r.CachedWritePer1K, &r.ImagePerUnit, &r.AudioPerMin,
			&r.Currency, &r.TierFlexMultiplier, &r.TierPriorityMultiplier, &r.TierFastMultiplier, &r.UpdatedAt,
		); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out
}

// UpsertPricing inserts or replaces a row.
func UpsertPricing(row ModelPricingRow) error {
	if pricingDB == nil {
		return sql.ErrConnDone
	}
	if row.Currency == "" {
		row.Currency = "USD"
	}
	if row.TierFlexMultiplier == 0 {
		row.TierFlexMultiplier = defaultPricing.TierFlexMultiplier
	}
	if row.TierPriorityMultiplier == 0 {
		row.TierPriorityMultiplier = defaultPricing.TierPriorityMultiplier
	}
	if row.TierFastMultiplier == 0 {
		row.TierFastMultiplier = defaultPricing.TierFastMultiplier
	}
	if row.UpdatedAt == 0 {
		row.UpdatedAt = time.Now().Unix()
	}
	_, err := pricingDB.Exec(
		`
INSERT INTO model_pricing (model_id, display_name, input_per_1k, output_per_1k, reason_per_1k, cached_read_per_1k, cached_write_per_1k, image_per_unit, audio_per_min, currency, tier_flex_multiplier, tier_priority_multiplier, tier_fast_multiplier, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(model_id) DO UPDATE SET
display_name = excluded.display_name,
input_per_1k = excluded.input_per_1k,
output_per_1k = excluded.output_per_1k,
reason_per_1k = excluded.reason_per_1k,
cached_read_per_1k = excluded.cached_read_per_1k,
cached_write_per_1k = excluded.cached_write_per_1k,
image_per_unit = excluded.image_per_unit,
audio_per_min = excluded.audio_per_min,
currency = excluded.currency,
tier_flex_multiplier = excluded.tier_flex_multiplier,
tier_priority_multiplier = excluded.tier_priority_multiplier,
tier_fast_multiplier = excluded.tier_fast_multiplier,
updated_at = excluded.updated_at`,
		row.ModelID, row.DisplayName, row.InputPer1K, row.OutputPer1K, row.ReasonPer1K,
		row.CachedReadPer1K, row.CachedWritePer1K, row.ImagePerUnit, row.AudioPerMin,
		row.Currency, row.TierFlexMultiplier, row.TierPriorityMultiplier, row.TierFastMultiplier, row.UpdatedAt,
	)
	if err != nil {
		return err
	}
	ReloadPricing()
	return nil
}

// DeletePricing removes a row.
func DeletePricing(modelID string) error {
	if pricingDB == nil {
		return sql.ErrConnDone
	}
	if _, err := pricingDB.Exec(`DELETE FROM model_pricing WHERE model_id = ?`, modelID); err != nil {
		return err
	}
	ReloadPricing()
	return nil
}
